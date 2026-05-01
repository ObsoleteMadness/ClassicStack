// Package zip defines the Zone Information Protocol wire constants:
// DDP type, statically-assigned socket, function codes (Query/Reply/
// GetNetInfo/ExtReply), GetNetInfo flag bits, and the ATP-carried ZIP
// function codes used in TReq UserBytes.
//
// This package is wire-format only. The ZIP responding/sending state
// machines live in service/zip.
//
// References:
//   - Inside Macintosh: Networking, Chapter 8
//     https://dev.os9.ca/techpubs/mac/Networking/Networking-167.html
package zip

const (
	// SAS is the statically-assigned ZIP socket.
	SAS = 6
	// DDPType is the DDP packet type for ZIP messages.
	DDPType = 6

	// ZIP function codes (in the first data byte of a ZIP-over-DDP packet).
	FuncQuery         = 1
	FuncReply         = 2
	FuncGetNetInfoReq = 5
	FuncGetNetInfoRep = 6
	FuncExtReply      = 8

	// GetNetInfo flag bits.
	GetNetInfoZoneInvalid  = 0x80
	GetNetInfoUseBroadcast = 0x40
	GetNetInfoOnlyOneZone  = 0x20

	// ATP-carried ZIP function codes (in TReq UserBytes high byte).
	ATPDDPType          = 3
	ATPFuncTReq         = 0x40
	ATPFuncTResp        = 0x80
	ATPEOM              = 0x10
	ATPGetMyZone        = 7
	ATPGetZoneList      = 8
	ATPGetLocalZoneList = 9
)
