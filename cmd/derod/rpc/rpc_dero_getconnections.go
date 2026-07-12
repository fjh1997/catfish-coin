// Copyright 2017-2021 DERO Project. All rights reserved.
// Use of this source code in any form is governed by RESEARCH license.
// license can be found in the LICENSE file.

package rpc

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/deroproject/derohe/p2p"
	"github.com/deroproject/derohe/rpc"
)

func GetConnections(ctx context.Context) (result rpc.GetConnections_Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic occured. stack trace %s", debug.Stack())
		}
	}()

	in, out := p2p.Peer_Direction_Count()
	views := p2p.ActiveConnections()
	connections := make([]rpc.Connection_Info, 0, len(views))
	for _, v := range views {
		connections = append(connections, rpc.Connection_Info{
			Address:       v.Address,
			Direction:     v.Direction,
			PeerID:        v.PeerID,
			Port:          v.Port,
			Height:        v.Height,
			StableHeight:  v.StableHeight,
			TopoHeight:    v.TopoHeight,
			LatencyMS:     v.LatencyMS,
			BytesIn:       v.BytesIn,
			BytesOut:      v.BytesOut,
			Tag:           v.Tag,
			DaemonVersion: v.DaemonVersion,
			Protocol:      v.Protocol,
			SyncNode:      v.SyncNode,
			StateHash:     v.StateHash,
		})
	}

	result.Incoming_connections_count = in
	result.Outgoing_connections_count = out
	result.Connections = connections
	result.Local_Port = uint32(p2p.P2P_Port)
	result.Advertised_Port = p2p.AdvertisedPort()
	result.External_Address = p2p.ExternalEndpointString()
	result.Status = "OK"
	return
}
