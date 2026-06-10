// Package zip implements the Zone Information Protocol as core router services: a responding
// service (socket 6) answering ZIP queries, GetNetInfo, and the ATP-carried GetMyZone /
// GetZoneList / GetLocalZones, plus a sending service that queries for zones of networks the
// zone information table does not yet know.
//
// Wire constants follow Inside Macintosh: Networking, Chapter 8. Ring: CORE — big-endian is
// hand-rolled (no encoding/binary, §1); zone-name case-folding uses core/encoding.
package zip

import (
	"github.com/ObsoleteMadness/ClassicStack/core/encoding"
)

const (
	// SAS is the statically-assigned ZIP socket.
	SAS = 6
	// DDPType is the DDP packet type for ZIP messages.
	DDPType = 6

	// FuncQuery / FuncReply / FuncGetNetInfoReq / FuncGetNetInfoRep / FuncExtReply are ZIP
	// function codes (the first data byte of a ZIP-over-DDP packet).
	FuncQuery         = 1
	FuncReply         = 2
	FuncGetNetInfoReq = 5
	FuncGetNetInfoRep = 6
	FuncExtReply      = 8

	// GetNetInfo flag bits.
	GetNetInfoZoneInvalid  = 0x80
	GetNetInfoUseBroadcast = 0x40
	GetNetInfoOnlyOneZone  = 0x20

	// ATP-carried ZIP function codes (in the TReq UserBytes / control fields).
	ATPDDPType          = 3
	ATPFuncTReq         = 0x40
	ATPFuncTResp        = 0x80
	ATPEOM              = 0x10
	ATPGetMyZone        = 7
	ATPGetZoneList      = 8
	ATPGetLocalZoneList = 9
)

// be16 reads a big-endian uint16.
func be16(b []byte) uint16 { return uint16(b[0])<<8 | uint16(b[1]) }

// toUCase upper-folds a zone name for case-insensitive comparison (MacRoman case table).
func toUCase(input []byte) []byte { return encoding.MacRomanToUpper(input) }

// multicastAddresser is an optional RoutedPort capability: an EtherTalk port can compute the
// multicast hardware address for a zone (used in GetNetInfo replies). Ports without it cause
// ZIP to set the use-broadcast flag instead.
type multicastAddresser interface {
	MulticastAddress(zoneName []byte) []byte
}
