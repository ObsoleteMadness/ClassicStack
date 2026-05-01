//go:build afp || all

package asp

import (
	pasp "github.com/pgodw/omnitalk/protocol/asp"
)

// SPFunction codes.
const (
	SPFuncCloseSess     = pasp.SPFuncCloseSess
	SPFuncCommand       = pasp.SPFuncCommand
	SPFuncGetStatus     = pasp.SPFuncGetStatus
	SPFuncOpenSess      = pasp.SPFuncOpenSess
	SPFuncTickle        = pasp.SPFuncTickle
	SPFuncWrite         = pasp.SPFuncWrite
	SPFuncWriteContinue = pasp.SPFuncWriteContinue
	SPFuncAttention     = pasp.SPFuncAttention
)

// Version + timers.
const (
	ASPVersion                = pasp.Version
	TickleInterval            = pasp.TickleInterval
	SessionMaintenanceTimeout = pasp.SessionMaintenanceTimeout
)

// Error codes.
const (
	SPErrorNoError        = pasp.SPErrorNoError
	SPErrorBadVersNum     = pasp.SPErrorBadVersNum
	SPErrorBufTooSmall    = pasp.SPErrorBufTooSmall
	SPErrorNoMoreSessions = pasp.SPErrorNoMoreSessions
	SPErrorNoServers      = pasp.SPErrorNoServers
	SPErrorParamErr       = pasp.SPErrorParamErr
	SPErrorServerBusy     = pasp.SPErrorServerBusy
	SPErrorSessClosed     = pasp.SPErrorSessClosed
	SPErrorSizeErr        = pasp.SPErrorSizeErr
	SPErrorTooManyClients = pasp.SPErrorTooManyClients
	SPErrorNoAck          = pasp.SPErrorNoAck
)

// AFP attention codes.
const AspAttnServerGoingDown = pasp.AspAttnServerGoingDown

// ATP-derived size constants.
const (
	ATPMaxData    = pasp.ATPMaxData
	ATPMaxPackets = pasp.ATPMaxPackets
	QuantumSize   = pasp.QuantumSize
)

// Wire types.
type (
	GetParmsResult      = pasp.GetParmsResult
	OpenSessPacket      = pasp.OpenSessPacket
	OpenSessReplyPacket = pasp.OpenSessReplyPacket
	CloseSessPacket     = pasp.CloseSessPacket
	GetStatusPacket     = pasp.GetStatusPacket
	CommandPacket       = pasp.CommandPacket
	WritePacket         = pasp.WritePacket
	WriteContinuePacket = pasp.WriteContinuePacket
	TicklePacket        = pasp.TicklePacket
	AttentionPacket     = pasp.AttentionPacket
)

// Parse helpers.
var (
	ParseOpenSessPacket     = pasp.ParseOpenSessPacket
	ParseCloseSessPacket    = pasp.ParseCloseSessPacket
	ParseGetStatusPacket    = pasp.ParseGetStatusPacket
	ParseCommandPacket      = pasp.ParseCommandPacket
	ParseWritePacket        = pasp.ParseWritePacket
	CloseSessReplyUserData  = pasp.CloseSessReplyUserData
)
