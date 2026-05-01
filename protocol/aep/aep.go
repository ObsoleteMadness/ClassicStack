// Package aep defines the AppleTalk Echo Protocol wire constants:
// statically-assigned socket, DDP type, and the request/reply command
// bytes carried in the first byte of the AEP payload.
//
// This package is wire-format only. The AEP service implementation
// (responder goroutine, router wiring) lives in service/aep.
//
// References:
//   - Inside Macintosh: Networking, Chapter 3
//     https://dev.os9.ca/techpubs/mac/Networking/Networking-115.html
package aep

const (
	// Socket is the statically-assigned AEP socket number.
	Socket = 4
	// DDPType is the DDP packet type for AEP packets.
	DDPType = 4

	// CmdRequest is the AEP command byte for an echo request.
	CmdRequest = 1
	// CmdReply is the AEP command byte for an echo reply.
	CmdReply = 2
)
