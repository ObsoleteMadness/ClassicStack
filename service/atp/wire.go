// Package atp wire-format re-exports.
//
// The wire format (header layout, control-bit constants, codec) lives in
// protocol/atp. This file re-exports those symbols under their historical
// names so the state-machine code in this package and its callers don't
// need to spell out an import alias for every reference.
package atp

import (
	patp "github.com/pgodw/omnitalk/protocol/atp"
)

// Header type.
type ATPHeader = patp.Header

// Function-code helpers.
type FuncCode = patp.FuncCode

const (
	FuncTReq  = patp.FuncTReq
	FuncTResp = patp.FuncTResp
	FuncTRel  = patp.FuncTRel
)

// Control-byte bit masks.
const (
	TREQ     = patp.TREQ
	TRESP    = patp.TRESP
	TREL     = patp.TREL
	XO       = patp.XO
	EOM      = patp.EOM
	STS      = patp.STS
	FuncMask = patp.FuncMask
)

// TRel timeout indicator.
type TRelTimeout = patp.TRelTimeout

const (
	TRel30s = patp.TRel30s
	TRel1m  = patp.TRel1m
	TRel2m  = patp.TRel2m
	TRel4m  = patp.TRel4m
	TRel8m  = patp.TRel8m
)

// Protocol limits and DDP type.
const (
	MaxResponsePackets = patp.MaxResponsePackets
	MaxATPData         = patp.MaxATPData
	DDPTypeATP         = patp.DDPType
	ATPHeaderSize      = patp.HeaderSize
)
