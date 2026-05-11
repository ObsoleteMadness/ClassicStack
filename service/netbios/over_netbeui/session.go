package over_netbeui

import protocol "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"

type sessionState = protocol.SessionState

const (
	sessionStateInit    = protocol.SessionStateInit
	sessionStateActive  = protocol.SessionStateActive
	sessionStateClosing = protocol.SessionStateClosing
	sessionStateClosed  = protocol.SessionStateClosed
)

type session = protocol.Session[[6]byte]

type sessionTable = protocol.SessionTable[[6]byte]

func newSessionTable() *sessionTable {
	return protocol.NewSessionTable[[6]byte](1, 254)
}
