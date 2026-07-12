// Copyright 2017-2021 DERO Project. All rights reserved.
// Use of this source code in any form is governed by RESEARCH license.

package p2p

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// listenUDP is the shared P2P UDP socket (also used by KCP). We keep the
// pointer so hole-punch probes can be sent from the same NAT mapping that
// STUN measured — same idea as Tailscale magicsock.
var listenUDP *net.UDPConn

var (
	punchRecentMu sync.Mutex
	punchRecent   = map[string]int64{} // pairKey -> unix deadline

	// Active punch jobs keyed by remote Peer_ID (Tailscale-style continuous disco).
	activePunchMu sync.Mutex
	activePunch   = map[uint64]*punchJob{}

	// One shared-socket dial attempt per remote endpoint (flooded TLS kills handshake).
	punchDialOnce sync.Map // endpoint -> unix deadline
	prflxRelayOnce sync.Map // peerID -> unix deadline
)

// discoMagic marks plaintext NAT probes multiplexed on the KCP socket.
// Must be checked BEFORE AES decrypt (see Listener.PacketHook).
var discoMagic = []byte("MOEFISH-PUNCH1")

type punchJob struct {
	mu        sync.Mutex
	peerID    uint64
	targets   []string
	iAmDialer bool
	nonce     uint64
	until     time.Time
}

func (j *punchJob) snapshotTargets() []string {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.targets...)
}

func clearBackoff(ip string) {
	backoff_mutex.Lock()
	delete(backoff, ParseIPNoError(ip))
	backoff_mutex.Unlock()
}

func formatHostPort(ip string, port uint32) string {
	if ip == "" || port == 0 {
		return ""
	}
	if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() == nil {
		return fmt.Sprintf("[%s]:%d", ip, port)
	}
	return fmt.Sprintf("%s:%d", ip, port)
}

func stunEndpointsFromFlags(flags []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range flags {
		if len(f) > 5 && f[:5] == "stun:" {
			ep := f[5:]
			if host, port, err := net.SplitHostPort(ep); err == nil && host != "" && port != "" && !seen[ep] {
				seen[ep] = true
				out = append(out, ep)
			}
		}
	}
	return out
}

func stunEndpointFromFlags(flags []string) string {
	eps := stunEndpointsFromFlags(flags)
	if len(eps) == 0 {
		return ""
	}
	return eps[0]
}

// observedEndpoint is the address as seen by this node (seed path).
func observedEndpoint(c *Connection) string {
	if c == nil || c.Port == 0 {
		return ""
	}
	return formatHostPort(Address(c), c.Port)
}

// dialableEndpoints returns ICE-style candidates: all STUN hits first, then
// seed-observed. Multi-egress NATs often have different public IPs per path.
func dialableEndpoints(c *Connection) (primary string, alts []string) {
	observed := observedEndpoint(c)
	seen := map[string]bool{}
	var list []string
	add := func(ep string) {
		if ep == "" || seen[ep] {
			return
		}
		seen[ep] = true
		list = append(list, ep)
	}
	if c != nil {
		add(c.ExternalAddr)
		for _, ep := range c.ExternalAddrs {
			add(ep)
		}
	}
	add(observed)
	if len(list) == 0 {
		return "", nil
	}
	return list[0], list[1:]
}

func dialableEndpoint(c *Connection) string {
	primary, _ := dialableEndpoints(c)
	return primary
}

func recordDialableFromHandshake(c *Connection, localPort uint32, peerID uint64, flags []string) {
	if c == nil || localPort == 0 || localPort > 65535 {
		return
	}
	c.Port = localPort
	c.Peer_ID = peerID
	stunEPs := stunEndpointsFromFlags(flags)
	if len(stunEPs) > 0 {
		c.ExternalAddr = stunEPs[0]
		c.ExternalAddrs = append([]string(nil), stunEPs...)
		for _, ep := range stunEPs {
			Peer_AddDialable(ParseIPNoError(ep), portFromAddr(ep), peerID)
		}
	}
	observed := formatHostPort(Address(c), localPort)
	if observed != "" {
		Peer_AddDialable(ParseIPNoError(observed), localPort, peerID)
	}
}

func FindConnectionByPeerID(id uint64) *Connection {
	var found *Connection
	connection_map.Range(func(k, value interface{}) bool {
		v := value.(*Connection)
		if atomic.LoadUint32(&v.State) == HANDSHAKE_PENDING {
			return true
		}
		if atomic.LoadUint64(&v.Peer_ID) == id && id != GetPeerID() {
			found = v
			return false
		}
		return true
	})
	return found
}

func pairKey(a, b uint64) string {
	if a > b {
		a, b = b, a
	}
	return fmt.Sprintf("%d:%d", a, b)
}

func shouldIntroduce(a, b uint64) bool {
	key := pairKey(a, b)
	now := time.Now().Unix()
	punchRecentMu.Lock()
	defer punchRecentMu.Unlock()
	if until, ok := punchRecent[key]; ok && until > now {
		return false
	}
	punchRecent[key] = now + 8 // at most once per 8s per pair (was 20s)
	return true
}


// introduce_peers_loop is the seed-side coordinator (any node with multiple
// dialable peers can run it). It tells each pair to punch toward the other.
func introduce_peers_loop() {
	type cand struct {
		c       *Connection
		primary string
		alts    []string
		id      uint64
	}
	var list []cand
	connection_map.Range(func(k, value interface{}) bool {
		v := value.(*Connection)
		if atomic.LoadUint32(&v.State) == HANDSHAKE_PENDING {
			return true
		}
		id := atomic.LoadUint64(&v.Peer_ID)
		if id == 0 || id == GetPeerID() {
			return true
		}
		primary, alts := dialableEndpoints(v)
		if primary == "" {
			return true
		}
		list = append(list, cand{c: v, primary: primary, alts: alts, id: id})
		return true
	})
	if len(list) < 2 {
		return
	}

	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			a, b := list[i], list[j]
			if !shouldIntroduce(a.id, b.id) {
				continue
			}
			var nonceBuf [8]byte
			_, _ = rand.Read(nonceBuf[:])
			nonce := binary.LittleEndian.Uint64(nonceBuf[:])
			ts := time.Now().UnixMilli()

			bAlt, bExtra := "", b.alts
			if len(b.alts) > 0 {
				bAlt = b.alts[0]
				bExtra = b.alts[1:]
			}
			aAlt, aExtra := "", a.alts
			if len(a.alts) > 0 {
				aAlt = a.alts[0]
				aExtra = a.alts[1:]
			}

			sendPunch(a.c, Punch_Struct{
				Peer_ID:    b.id,
				Addr:       b.primary,
				Alt_Addr:   bAlt,
				Cand_Addrs: bExtra,
				Self_Addr:  a.primary,
				Nonce:      nonce,
				UnixMilli:  ts,
			})
			sendPunch(b.c, Punch_Struct{
				Peer_ID:    a.id,
				Addr:       a.primary,
				Alt_Addr:   aAlt,
				Cand_Addrs: aExtra,
				Self_Addr:  b.primary,
				Nonce:      nonce,
				UnixMilli:  ts,
			})


			logger.Info("Coordinated NAT punch", "a", a.primary, "a_alts", a.alts, "b", b.primary, "b_alts", b.alts, "nonce", nonce)
		}
	}
}

func sendPunch(c *Connection, punch Punch_Struct) {
	if c == nil || c.Client == nil {
		return
	}
	fill_common(&punch.Common)
	go func() {
		defer handle_connection_panic(c)
		var reply Dummy
		_ = c.Client.Call("Peer.NotifyPunch", punch, &reply)
	}()
}

// NotifyPunch is received by desktop nodes from the seed coordinator.
func (c *Connection) NotifyPunch(request Punch_Struct, response *Dummy) error {
	defer handle_connection_panic(c)
	fill_common(&response.Common)
	go executePunch(request)
	return nil
}

func punchTargets(request Punch_Struct) []string {
	var out []string
	seen := map[string]bool{}
	add := func(addr string) {
		if addr == "" || seen[addr] {
			return
		}
		seen[addr] = true
		out = append(out, addr)
	}
	add(request.Addr)
	add(request.Alt_Addr)
	for _, addr := range request.Cand_Addrs {
		add(addr)
	}
	return out
}

func anyTargetConnected(targets []string) string {
	for _, addr := range targets {
		if IsAddressConnected(ParseIPNoError(addr)) {
			return addr
		}
	}
	return ""
}

func registerPunchJob(job *punchJob) {
	activePunchMu.Lock()
	activePunch[job.peerID] = job
	activePunchMu.Unlock()
}

func unregisterPunchJob(job *punchJob) {
	activePunchMu.Lock()
	if cur, ok := activePunch[job.peerID]; ok && cur == job {
		delete(activePunch, job.peerID)
	}
	activePunchMu.Unlock()
}

func getPunchJob(peerID uint64) *punchJob {
	activePunchMu.Lock()
	defer activePunchMu.Unlock()
	return activePunch[peerID]
}

func addPunchTarget(job *punchJob, addr string) bool {
	if job == nil || addr == "" {
		return false
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	for _, t := range job.targets {
		if t == addr {
			return false
		}
	}
	job.targets = append(job.targets, addr)
	return true
}

// tryPunchDial starts at most one shared-socket dial per endpoint for ~40s.
func tryPunchDial(endpoint string) bool {
	if endpoint == "" {
		return false
	}
	now := time.Now().Unix()
	if v, ok := punchDialOnce.Load(endpoint); ok {
		if until, _ := v.(int64); until > now {
			return false
		}
	}
	punchDialOnce.Store(endpoint, now+40)
	logger.Info("NAT punch dial once", "endpoint", endpoint)
	go connect_with_endpoint_opts(endpoint, false, true)
	return true
}

func shouldRelayPrflx(peerID uint64) bool {
	now := time.Now().Unix()
	key := fmt.Sprintf("%d", peerID)
	if v, ok := prflxRelayOnce.Load(key); ok {
		if until, _ := v.(int64); until > now {
			return false
		}
	}
	prflxRelayOnce.Store(key, now+5)
	return true
}

func executePunch(request Punch_Struct) {
	defer globalsRecover()
	targets := punchTargets(request)
	if len(targets) == 0 || request.Peer_ID == 0 || request.Peer_ID == GetPeerID() {
		return
	}
	if hit := anyTargetConnected(targets); hit != "" {
		logger.V(2).Info("punch skipped, already connected", "addr", hit)
		return
	}

	for _, addr := range targets {
		clearBackoff(addr)
		Peer_AddDialable(ParseIPNoError(addr), portFromAddr(addr), request.Peer_ID)
	}

	logger.Info("Executing NAT punch", "targets", targets, "peer_id", request.Peer_ID, "nonce", request.Nonce)


	// Align roughly with coordinator clock (best-effort).
	if request.UnixMilli > 0 {
		delay := time.Until(time.UnixMilli(request.UnixMilli + 200))
		if delay > 0 && delay < 2*time.Second {
			time.Sleep(delay)
		}
	}

	// Lower Peer_ID dials (TLS client on shared listen socket). Higher Peer_ID
	// only probes and waits for Accept (TLS server) — avoids dual-client TLS.
	iAmDialer := GetPeerID() < request.Peer_ID
	logger.Info("NAT punch role", "dialer", iAmDialer, "self", GetPeerID(), "peer", request.Peer_ID)

	job := &punchJob{
		peerID:    request.Peer_ID,
		targets:   append([]string(nil), targets...),
		iAmDialer: iAmDialer,
		nonce:     request.Nonce,
		until:     time.Now().Add(25 * time.Second),
	}
	registerPunchJob(job)
	defer unregisterPunchJob(job)

	// Simultaneous open against every candidate (multi-egress ICE-style).
	// Tailscale keeps disco continuous; we burst then sustain for the job window.
	for i := 0; i < 12; i++ {
		for _, addr := range job.snapshotTargets() {
			punchProbeTo(addr, request.Peer_ID, request.Nonce)
		}
		time.Sleep(30 * time.Millisecond)
	}

	// Dial ALL candidates once in parallel (Tailscale probes all paths).
	// Previously sequential waits + per-disco re-dial destroyed TLS.
	if iAmDialer {
		for _, addr := range job.snapshotTargets() {
			tryPunchDial(addr)
		}
	}

	deadline := job.until
	for time.Now().Before(deadline) {
		targetsNow := job.snapshotTargets()
		if hit := anyTargetConnected(targetsNow); hit != "" {
			logger.Info("NAT punch succeeded", "target", hit)
			return
		}
		for _, addr := range targetsNow {
			punchProbeTo(addr, request.Peer_ID, request.Nonce)
		}
		time.Sleep(200 * time.Millisecond)
	}

	sendRelayDisco(request.Peer_ID, "punch-ack", []byte(ExternalEndpointString()))
	time.Sleep(2 * time.Second)
	finalTargets := job.snapshotTargets()
	if hit := anyTargetConnected(finalTargets); hit != "" {
		logger.Info("NAT punch succeeded after relay ack", "target", hit)
		return
	}
	logger.Info("Direct punch incomplete; chain sync stays on seed relay", "targets", finalTargets)
}

func portFromAddr(addr string) uint32 {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	var p int
	fmt.Sscanf(portStr, "%d", &p)
	if p < 0 || p > 65535 {
		return 0
	}
	return uint32(p)
}

func encodeDisco(toPeerID, nonce uint64) []byte {
	buf := make([]byte, len(discoMagic)+24)
	copy(buf, discoMagic)
	binary.LittleEndian.PutUint64(buf[len(discoMagic):], GetPeerID())
	binary.LittleEndian.PutUint64(buf[len(discoMagic)+8:], toPeerID)
	binary.LittleEndian.PutUint64(buf[len(discoMagic)+16:], nonce)
	return buf
}

func parseDisco(data []byte) (fromID, toID, nonce uint64, ok bool) {
	need := len(discoMagic) + 24
	if len(data) < need {
		return 0, 0, 0, false
	}
	if string(data[:len(discoMagic)]) != string(discoMagic) {
		return 0, 0, 0, false
	}
	fromID = binary.LittleEndian.Uint64(data[len(discoMagic):])
	toID = binary.LittleEndian.Uint64(data[len(discoMagic)+8:])
	nonce = binary.LittleEndian.Uint64(data[len(discoMagic)+16:])
	return fromID, toID, nonce, true
}

func punchProbe(addr string) {
	punchProbeTo(addr, 0, 0)
}

func punchProbeTo(addr string, toPeerID, nonce uint64) {
	if listenUDP == nil {
		return
	}
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return
	}
	_, _ = listenUDP.WriteToUDP(encodeDisco(toPeerID, nonce), raddr)
}

// handleInboundDisco is the magicsock PacketHook: learn peer-reflexive
// endpoints from inbound plaintext punch (MappingVariesByDestIP case).
func handleInboundDisco(data []byte, addr net.Addr) bool {
	fromID, toID, nonce, ok := parseDisco(data)
	if !ok {
		return false
	}
	if toID != 0 && toID != GetPeerID() {
		return true // consume foreign disco noise
	}
	src := addr.String()
	if u, ok := addr.(*net.UDPAddr); ok {
		src = u.String()
	}
	logger.Info("NAT disco prflx learned", "from", fromID, "src", src, "nonce", nonce)


	if fromID == 0 || fromID == GetPeerID() {
		return true
	}

	clearBackoff(src)
	Peer_AddDialable(ParseIPNoError(src), portFromAddr(src), fromID)

	job := getPunchJob(fromID)
	isNew := false
	if job != nil {
		isNew = addPunchTarget(job, src)
		if isNew {
			logger.Info("NAT punch added prflx candidate", "peer", fromID, "addr", src)
		}
	}

	// Keep the hole warm toward the observed source (Tailscale disco ping).
	punchProbeTo(src, fromID, nonce)

	// Dialer initiates TLS/KCP once on the reflexive address.
	if (job != nil && job.iAmDialer) || (job == nil && GetPeerID() < fromID) {
		tryPunchDial(src)
	}

	// Tell peer (via seed) which address we observed — CallMeMaybe-style, rate-limited.
	if shouldRelayPrflx(fromID) {
		sendRelayDisco(fromID, "prflx", []byte(src))
	}
	return true
}

func globalsRecover() {
	if r := recover(); r != nil {
		logger.V(1).Error(nil, "punch panic", "r", r)
	}
}

// Relay forwards an opaque frame to To_ID if we have that peer (seed path).
func (c *Connection) Relay(request Relay_Struct, response *Dummy) error {
	defer handle_connection_panic(c)
	fill_common(&response.Common)
	if len(request.Payload) > 16*1024 {
		return fmt.Errorf("relay payload too large")
	}
	if request.To_ID == 0 || request.To_ID == GetPeerID() {
		return fmt.Errorf("invalid relay destination")
	}
	dest := FindConnectionByPeerID(request.To_ID)
	if dest == nil || dest.Client == nil {
		return fmt.Errorf("relay destination offline")
	}
	request.From_ID = atomic.LoadUint64(&c.Peer_ID)
	fill_common(&request.Common)
	go func() {
		defer handle_connection_panic(dest)
		var reply Dummy
		_ = dest.Client.Call("Peer.NotifyRelay", request, &reply)
	}()
	return nil
}

// NotifyRelay is delivered to the destination peer via the seed.
func (c *Connection) NotifyRelay(request Relay_Struct, response *Dummy) error {
	defer handle_connection_panic(c)
	fill_common(&response.Common)
	switch request.Kind {
	case "prflx":
		// Peer says "I observe path at payload". Payload is NOT From's dial address.
		addr := string(request.Payload)
		logger.Info("Relay prflx hint", "from", request.From_ID, "observed", addr)
	case "punch-ack", "disco":
		addr := string(request.Payload)
		if addr != "" && request.From_ID != 0 {
			clearBackoff(addr)
			Peer_AddDialable(ParseIPNoError(addr), portFromAddr(addr), request.From_ID)
			go func() {
				defer globalsRecover()
				// Keep the hole warm; do NOT DialKCP again — that closes any
				// in-flight shared-socket TLS handshake to the same peer.
				punchProbeTo(addr, request.From_ID, 0)
			}()
		}
	case "ping":
		// keep-alive through relay; no-op beyond proving path
	}
	logger.V(1).Info("Relay frame received", "kind", request.Kind, "from", request.From_ID, "bytes", len(request.Payload))
	return nil
}

func sendRelayDisco(toID uint64, kind string, payload []byte) {
	// Prefer any active connection that looks like a coordinator (incoming to us
	// means we are a leaf; outgoing priority/seed is the usual path).
	var seed *Connection
	connection_map.Range(func(k, value interface{}) bool {
		v := value.(*Connection)
		if atomic.LoadUint32(&v.State) == HANDSHAKE_PENDING {
			return true
		}
		if !v.Incoming { // our outbound to seed/priority
			seed = v
			return false
		}
		return true
	})
	if seed == nil || seed.Client == nil {
		return
	}
	req := Relay_Struct{
		From_ID: GetPeerID(),
		To_ID:   toID,
		Kind:    kind,
		Payload: payload,
	}
	fill_common(&req.Common)
	go func() {
		defer handle_connection_panic(seed)
		var reply Dummy
		if err := seed.Client.Call("Peer.Relay", req, &reply); err != nil {
			logger.V(2).Info("relay send failed", "err", err, "kind", kind)
		}
	}()
}
