package over_netbeui

import protocol "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"

const (
	sessionStateActive = protocol.SessionStateActive
	sessionStateClosed = protocol.SessionStateClosed
)

type session = protocol.Session[[6]byte]

type sessionTable = protocol.SessionTable[[6]byte]

func newSessionTable() *sessionTable {
	return protocol.NewSessionTable[[6]byte](1, 254)
}
