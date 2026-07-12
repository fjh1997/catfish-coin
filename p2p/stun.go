// Copyright 2017-2021 DERO Project. All rights reserved.
// Use of this source code in any form is governed by RESEARCH license.

package p2p

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"
)

// Public STUN endpoints — query ALL of them and keep every distinct mapping.
// Multi-egress NATs may answer with different public IPs depending on the
// path taken to each STUN server (ICE-style candidate gathering).
var defaultSTUNServers = []string{
	"stun.cloudflare.com:3478",
	"stun.l.google.com:19302",
	"stun1.l.google.com:19302",
	"stun2.l.google.com:19302",
	"stun3.l.google.com:19302",
	"stun4.l.google.com:19302",
	"stun.nextcloud.com:3478",
	"stun.stunprotocol.org:3478",
	"stun.sipgate.net:3478",
}

const (
	stunMagicCookie       = 0x2112A442
	stunMsgBindingRequest = 0x0001
	stunMsgBindingSuccess = 0x0101
	attrMappedAddress     = 0x0001
	attrXorMappedAddress  = 0x0020
)

var (
	externalMu        sync.RWMutex
	externalIP        net.IP
	externalPort      int
	externalSource    string
	externalEndpoints []string // all unique ip:port from multi-STUN
)

// ExternalEndpoint returns the primary STUN-learned public ip:port, if available.
func ExternalEndpoint() (ip net.IP, port int, ok bool) {
	externalMu.RLock()
	defer externalMu.RUnlock()
	if externalIP == nil || externalPort <= 0 {
		return nil, 0, false
	}
	return append(net.IP(nil), externalIP...), externalPort, true
}

// ExternalEndpoints returns every distinct STUN mapping we learned.
func ExternalEndpoints() []string {
	externalMu.RLock()
	defer externalMu.RUnlock()
	out := make([]string, len(externalEndpoints))
	copy(out, externalEndpoints)
	return out
}

func setExternalEndpoint(ip net.IP, port int, source string) {
	externalMu.Lock()
	externalIP = append(net.IP(nil), ip...)
	externalPort = port
	externalSource = source
	externalMu.Unlock()
}

func addExternalEndpoint(ep string) {
	if ep == "" {
		return
	}
	externalMu.Lock()
	defer externalMu.Unlock()
	for _, e := range externalEndpoints {
		if e == ep {
			return
		}
	}
	externalEndpoints = append(externalEndpoints, ep)
}

// AdvertisedPort is what we put in handshake Local_Port.
// Prefer the STUN mapped port so other nodes dial the NAT mapping that
// belongs to our listen socket; fall back to the local listen port.
func AdvertisedPort() uint32 {
	if _, port, ok := ExternalEndpoint(); ok {
		return uint32(port)
	}
	return uint32(P2P_Port)
}

func ExternalEndpointString() string {
	ip, port, ok := ExternalEndpoint()
	if !ok {
		return ""
	}
	if ip4 := ip.To4(); ip4 != nil {
		return fmt.Sprintf("%s:%d", ip4.String(), port)
	}
	return fmt.Sprintf("[%s]:%d", ip.String(), port)
}

func formatEndpoint(ip net.IP, port int) string {
	if ip == nil || port <= 0 {
		return ""
	}
	if ip4 := ip.To4(); ip4 != nil {
		return fmt.Sprintf("%s:%d", ip4.String(), port)
	}
	return fmt.Sprintf("[%s]:%d", ip.String(), port)
}

// discoverExternalAddress runs STUN Binding requests on the P2P listen socket
// against every configured server and keeps all distinct mapped endpoints.
// Bounded wall time so ServeConn/shared-socket punch can start quickly
// (Tailscale keeps probing in the background; we prioritize listen readiness).
func discoverExternalAddress(conn *net.UDPConn) {
	if conn == nil {
		return
	}
	overall := time.Now().Add(4 * time.Second)
	_ = conn.SetDeadline(overall)
	defer conn.SetDeadline(time.Time{})

	var primarySet bool
	for _, server := range defaultSTUNServers {
		if time.Now().After(overall) {
			break
		}
		ip, port, err := doSTUNBindingRequest(conn, server)
		if err != nil {
			logger.V(1).Info("STUN probe failed", "server", server, "err", err)
			continue
		}
		ep := formatEndpoint(ip, port)
		addExternalEndpoint(ep)
		if !primarySet {
			setExternalEndpoint(ip, port, server)
			primarySet = true
			logger.Info("STUN mapped external endpoint", "endpoint", ep, "via", server)
		} else {
			logger.Info("STUN extra candidate", "endpoint", ep, "via", server)
		}
		// Two distinct mappings is enough to expose multi-egress NATs.
		if primarySet && len(ExternalEndpoints()) >= 2 {
			break
		}
	}
	if !primarySet {
		logger.Info("STUN unavailable; advertising local listen port only", "local_port", P2P_Port)
		return
	}
	logger.Info("STUN candidate set", "endpoints", ExternalEndpoints())
}

func doSTUNBindingRequest(conn *net.UDPConn, server string) (net.IP, int, error) {
	raddr, err := net.ResolveUDPAddr("udp", server)
	if err != nil {
		return nil, 0, err
	}

	var txID [12]byte
	if _, err := rand.Read(txID[:]); err != nil {
		return nil, 0, err
	}

	req := make([]byte, 20)
	binary.BigEndian.PutUint16(req[0:2], stunMsgBindingRequest)
	binary.BigEndian.PutUint16(req[2:4], 0) // length
	binary.BigEndian.PutUint32(req[4:8], stunMagicCookie)
	copy(req[8:20], txID[:])

	if _, err := conn.WriteToUDP(req, raddr); err != nil {
		return nil, 0, err
	}

	buf := make([]byte, 1500)
	deadline := time.Now().Add(1200 * time.Millisecond)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(350 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return nil, 0, err
		}
		if n < 20 {
			continue
		}
		if binary.BigEndian.Uint16(buf[0:2]) != stunMsgBindingSuccess {
			continue
		}
		if binary.BigEndian.Uint32(buf[4:8]) != stunMagicCookie {
			continue
		}
		if string(buf[8:20]) != string(txID[:]) {
			continue
		}
		ip, port, err := parseSTUNMappedAddress(buf[:n], txID)
		if err != nil {
			return nil, 0, err
		}
		return ip, port, nil
	}
	return nil, 0, fmt.Errorf("no STUN response from %s", server)
}

func parseSTUNMappedAddress(msg []byte, txID [12]byte) (net.IP, int, error) {
	if len(msg) < 20 {
		return nil, 0, fmt.Errorf("short stun message")
	}
	length := int(binary.BigEndian.Uint16(msg[2:4]))
	if len(msg) < 20+length {
		return nil, 0, fmt.Errorf("truncated stun message")
	}
	attrs := msg[20 : 20+length]
	for len(attrs) >= 4 {
		at := binary.BigEndian.Uint16(attrs[0:2])
		al := int(binary.BigEndian.Uint16(attrs[2:4]))
		attrs = attrs[4:]
		if len(attrs) < al {
			return nil, 0, fmt.Errorf("bad stun attr length")
		}
		value := attrs[:al]
		pad := (4 - (al % 4)) % 4
		attrs = attrs[al+pad:]

		switch at {
		case attrXorMappedAddress:
			return decodeXORMapped(value, txID)
		case attrMappedAddress:
			return decodeMapped(value)
		}
	}
	return nil, 0, fmt.Errorf("mapped address attribute missing")
}

func decodeMapped(value []byte) (net.IP, int, error) {
	if len(value) < 4 {
		return nil, 0, fmt.Errorf("short mapped address")
	}
	family := value[1]
	port := int(binary.BigEndian.Uint16(value[2:4]))
	switch family {
	case 0x01: // IPv4
		if len(value) < 8 {
			return nil, 0, fmt.Errorf("short ipv4 mapped address")
		}
		return net.IPv4(value[4], value[5], value[6], value[7]), port, nil
	case 0x02: // IPv6
		if len(value) < 20 {
			return nil, 0, fmt.Errorf("short ipv6 mapped address")
		}
		ip := make(net.IP, 16)
		copy(ip, value[4:20])
		return ip, port, nil
	default:
		return nil, 0, fmt.Errorf("unknown address family %d", family)
	}
}

func decodeXORMapped(value []byte, txID [12]byte) (net.IP, int, error) {
	if len(value) < 4 {
		return nil, 0, fmt.Errorf("short xor mapped address")
	}
	family := value[1]
	xport := binary.BigEndian.Uint16(value[2:4]) ^ uint16(stunMagicCookie>>16)
	switch family {
	case 0x01:
		if len(value) < 8 {
			return nil, 0, fmt.Errorf("short ipv4 xor mapped address")
		}
		raw := binary.BigEndian.Uint32(value[4:8]) ^ stunMagicCookie
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, raw)
		return net.IP(ip), int(xport), nil
	case 0x02:
		if len(value) < 20 {
			return nil, 0, fmt.Errorf("short ipv6 xor mapped address")
		}
		ip := make(net.IP, 16)
		copy(ip, value[4:20])
		var magic [4]byte
		binary.BigEndian.PutUint32(magic[:], stunMagicCookie)
		for i := 0; i < 4; i++ {
			ip[i] ^= magic[i]
		}
		for i := 0; i < 12; i++ {
			ip[4+i] ^= txID[i]
		}
		return ip, int(xport), nil
	default:
		return nil, 0, fmt.Errorf("unknown address family %d", family)
	}
}
