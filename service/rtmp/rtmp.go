package rtmp

import (
	"encoding/binary"

	"github.com/pgodw/omnitalk/protocol/ddp"

	"github.com/pgodw/omnitalk/service"
)

const (
	SAS                    = 1
	DDPTypeData            = 1
	DDPTypeRequest         = 5
	Version                = 0x82
	FuncRequest            = 1
	FuncRDRSplitHorizon    = 2
	FuncRDRNoSplitHorizon  = 3
	NotifyNeighborDistance = 31
)

func makeRoutingTableDatagramData(r service.Router, p interface {
	NetworkMin() uint16
	NetworkMax() uint16
	Network() uint16
	Node() uint8
	ExtendedNetwork() bool
}, splitHorizon bool) [][]byte {
	if p.NetworkMin() == 0 || p.NetworkMax() == 0 {
		return nil
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint16(header[0:2], p.Network())
	header[2] = 8
	header[3] = p.Node()
	var tuples [][]byte
	var thisNet []byte
	for _, item := range r.RoutingEntries() {
		e := item.Entry
		distance := e.Distance
		if item.Bad {
			distance = NotifyNeighborDistance
		}
		var tuple []byte
		if !e.ExtendedNetwork {
			tuple = []byte{byte(e.NetworkMin >> 8), byte(e.NetworkMin), byte(distance & 0x1F)}
		} else {
			tuple = []byte{byte(e.NetworkMin >> 8), byte(e.NetworkMin), byte(distance&0x1F) | 0x80, byte(e.NetworkMax >> 8), byte(e.NetworkMax), Version}
		}
		if p.ExtendedNetwork() && p.NetworkMin() == e.NetworkMin && p.NetworkMax() == e.NetworkMax {
			thisNet = tuple
		} else if e.Port == p && splitHorizon {
			continue
		} else {
			tuples = append(tuples, tuple)
		}
	}
	if p.ExtendedNetwork() && thisNet != nil {
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
