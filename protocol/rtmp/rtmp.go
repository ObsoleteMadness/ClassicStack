// Package rtmp defines the Routing Table Maintenance Protocol wire
// constants: statically-assigned socket, DDP types for data and request
// packets, RTMP version byte, function codes, and the special distance
// value used to advertise an unreachable network.
//
// This package is wire-format only. The RTMP responding/sending state
// machines and routing-table aging live in service/rtmp.
//
// References:
//   - Inside Macintosh: Networking, Chapter 5
//     https://dev.os9.ca/techpubs/mac/Networking/Networking-129.html
package rtmp

const (
	// SAS is the statically-assigned RTMP socket.
	SAS = 1
	// DDPTypeData is the DDP type for RTMP Data packets (routing tuples).
	DDPTypeData = 1
	// DDPTypeRequest is the DDP type for RTMP Request packets.
	DDPTypeRequest = 5
	// Version is the RTMP version byte present in tuple packets.
	Version = 0x82

	// Function codes inside Request packets.
	FuncRequest           = 1
	FuncRDRSplitHorizon   = 2
	FuncRDRNoSplitHorizon = 3

	// NotifyNeighborDistance is the distance value used to advertise that
	// a route has gone bad (Notify Neighbor).
	NotifyNeighborDistance = 31
)
