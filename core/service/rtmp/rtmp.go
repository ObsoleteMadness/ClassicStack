// Package rtmp implements the Routing Table Maintenance Protocol as core router services:
// a responding service (socket 1) that answers RTMP requests and folds learned routes into
// the table, a sending service that periodically advertises the routing table, and an aging
// service that ticks the routing table's RTMP aging machine.
//
// Wire constants follow Inside Macintosh: Networking, Chapter 5. Ring: CORE — big-endian is
// hand-rolled (no encoding/binary, which pulls reflect, §1).
package rtmp

import (
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/ddp"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
)

const (
	// SAS is the statically-assigned RTMP socket.
	SAS = 1
	// DDPTypeData is the DDP type for RTMP Data packets (routing tuples).
	DDPTypeData = 1
	// DDPTypeRequest is the DDP type for RTMP Request packets.
	DDPTypeRequest = 5
	// Version is the RTMP version byte present in tuple packets.
	Version = 0x82

	// FuncRequest asks a router for its network range (Request packet function 1).
	FuncRequest = 1
	// FuncRDRSplitHorizon asks for the full routing table with split-horizon applied.
	FuncRDRSplitHorizon = 2
	// FuncRDRNoSplitHorizon asks for the full routing table without split-horizon.
	FuncRDRNoSplitHorizon = 3

	// NotifyNeighborDistance advertises a network as unreachable (Notify Neighbor).
	NotifyNeighborDistance = 31
)

// makeRoutingTableDatagramData builds the RTMP Data datagrams advertised over port p: a
// header (p's network/node and own extended tuple) followed by neighbour tuples, split into
// DDP-sized datagrams. splitHorizon omits routes learned via p itself.
func makeRoutingTableDatagramData(r router.ServiceRouter, p router.RoutedPort, splitHorizon bool) [][]byte {
	if p.NetworkMin() == 0 || p.NetworkMax() == 0 {
		return nil
	}
	pExtended := router.PortIsExtended(p)
	header := []byte{byte(p.Network() >> 8), byte(p.Network()), 8, p.Node()}

	var tuples [][]byte
	var thisNet []byte
	for _, item := range r.RoutingTable().Entries() {
		e := item.Entry
		distance := e.Distance
		if item.Bad {
			distance = NotifyNeighborDistance
		}
		var tuple []byte
		if !e.ExtendedNetwork {
			tuple = []byte{byte(e.NetworkMin >> 8), byte(e.NetworkMin), byte(distance & 0x1F)}
		} else {
			tuple = []byte{byte(e.NetworkMin >> 8), byte(e.NetworkMin), byte(distance&0x1F) | 0x80,
				byte(e.NetworkMax >> 8), byte(e.NetworkMax), Version}
		}
		switch {
		case pExtended && p.NetworkMin() == e.NetworkMin && p.NetworkMax() == e.NetworkMax:
			thisNet = tuple
		case e.Port == p && splitHorizon:
			continue
		default:
			tuples = append(tuples, tuple)
		}
	}
	if pExtended && thisNet != nil {
		header = append(header, thisNet...)
	} else {
		header = append(header, 0, 0, Version)
	}

	var out [][]byte
	curr := append([]byte(nil), header...)
	for _, t := range tuples {
		if len(curr)+len(t) > ddp.MaxDataLength {
			out = append(out, curr)
			curr = append(append([]byte(nil), header...), t...)
		} else {
			curr = append(curr, t...)
		}
	}
	out = append(out, curr)
	return out
}
