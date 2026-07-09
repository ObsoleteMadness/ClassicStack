package ncp

// sap.go builds the NCP file server's SAP service entry. The actual Service
// Advertising Protocol broadcast/query machinery lives in the shared
// core/service/sap advertiser (one advertiser per runtime on IPX socket 0x0452,
// through which NCP and NB-IPX both advertise); NCP only supplies its entry — its
// type (File Server 0x0004), name, and NCP socket (0x0451) — for compose to register
// there. Keeping the advertiser shared avoids two handlers fighting for socket 0x0452.
//
// Reference: Novell SAP (IPX socket 0x0452); mars_nwe / ncpfs (CLAUDE.md #7).

import ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"

// SAPEntry builds the SAP service entry advertising this file server: the File Server
// type (0x0004), the server name, and the NCP service socket (0x0451). The IPX
// network/node are left zero for the shared advertiser to fill from the mini-router
// identity. Compose calls this and registers the result with the shared advertiser.
func (s *Service) SAPEntry() ncpproto.SAPEntry {
	return ncpproto.SAPEntry{
		Type:   ncpproto.SAPServerTypeFileServer,
		Name:   s.serverName(),
		Socket: ncpproto.NCPSocket,
		Hops:   1,
	}
}
