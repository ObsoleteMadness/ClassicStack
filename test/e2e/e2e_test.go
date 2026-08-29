package e2e

// e2e_test.go is the consolidated table: for every protocol×transport a server builder
// produces a connected client fs.ForkFS, and the shared file-operation battery
// (exerciseFileOps) runs against it. A single failing subtest names exactly which
// protocol×transport and which operation broke.

import (
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// cases enumerates every protocol×transport this harness can exercise in-process. The
// mapping to the real transports:
//   - afp/ddp       models AFP over LToUDP and EtherTalk (DDP payload is transport-agnostic)
//   - afp/dsi       real client dsi.Session TCP/DSI framing over a net.Pipe
//   - smb/direct    the message-level SMB circuit (direct-hosted family)
//   - smb/tcp       real client TCP/NBT framing over a net.Pipe
//   - smb/nbipx     real IPX port + NBIPX session engine over an inmem pair
//   - smb/nbf       real NetBEUI port + LLC2 responder + NBF session engine over an inmem pair
//   - ncp/ipx       NCP over an IPX-datagram bridge
//   - etherdfs/eth  EtherDFS over a raw-Ethernet inmem pair
var cases = []struct {
	name  string
	build func(t *testing.T) fs.ForkFS
	names fileNames
}{
	{"afp/ddp", afpServer, longNames},
	{"afp/dsi", afpDSIServer, longNames},
	{"smb/direct", smbBridgeServer, longNames},
	{"smb/tcp", smbTCPServer, longNames},
	{"smb/nbipx", smbNBIPXServer, longNames},
	{"smb/nbf", smbNBFServer, longNames},
	{"ncp/ipx", ncpServer, dosNames},
	{"etherdfs/eth", etherdfsServer, dosNames},
}

// TestAllProtocols_E2E stands up one classicstack server per protocol×transport and drives
// the full file-operation battery (create+forks → list → copy out → copy back → rename →
// delete → dir create/delete) through each client. This is the "exercise all client
// protocols and file operations against one server harness" gate.
func TestAllProtocols_E2E(t *testing.T) {
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			remote := tc.build(t)
			exerciseFileOps(t, remote, tc.names)
		})
	}
}
