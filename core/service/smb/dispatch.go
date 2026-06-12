package smb

import (
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// SMB status codes the spine returns ([MS-CIFS] §2.2 / [MS-ERREF]). The wire form
// is a 32-bit NTSTATUS when the request set SMB_FLAGS2_NT_STATUS, otherwise the
// DOS class/code is substituted by toWireStatus. Only the codes the
// session-establishment spine needs are enumerated; the FS command engine adds
// its own in a later slice.
const (
	statusSuccess        uint32 = 0x00000000
	statusNotSupported   uint32 = 0xC00000BB // STATUS_NOT_SUPPORTED
	statusBadNetworkName uint32 = 0xC00000CC // STATUS_BAD_NETWORK_NAME (no such share)
	statusSMBBadTID      uint32 = 0x00050002 // STATUS_SMB_BAD_TID
	statusAccessDenied   uint32 = 0xC0000022 // STATUS_ACCESS_DENIED
)

// ipcShareName is the virtual IPC$ tree always available for named-pipe/LANMAN
// use. A TREE_CONNECT to it binds a pipe-only tree (no filesystem share).
const ipcShareName = "IPC$"

// Dispatch decodes one SMB1 request frame and returns the response frame to send
// back, or nil to send nothing (the connectionless silent-drop case some
// commands need). It is transport-independent: the NetBIOS/transport seam decodes
// the session-message framing and calls Dispatch with the raw SMB message
// (starting at the "\xffSMB" header). Each connection owns one *smbSession.
//
// As of this slice the spine handles the session-establishment commands —
// NEGOTIATE, SESSION_SETUP_ANDX, TREE_CONNECT(_ANDX), TREE_DISCONNECT,
// LOGOFF_ANDX, ECHO. Filesystem commands (NT_CREATE_ANDX, READ/WRITE, CLOSE,
// TRANS2 find/query) return STATUS_NOT_SUPPORTED until the FS command-engine
// slice lands; an unparseable frame is dropped (nil) so a malformed packet cannot
// wedge the connection.
func (s *Service) Dispatch(sess *smbSession, req []byte) []byte {
	h, err := protocol.DecodeHeader(req)
	if err != nil {
		return nil // not an SMB frame — drop it
	}

	switch h.Command {
	case protocol.CommandNegotiate:
		return s.handleNegotiate(h, req)
	case protocol.CommandSessionSetupAndX:
		return s.handleSessionSetup(sess, h, req)
	case protocol.CommandTreeConnectAndX:
		return s.handleTreeConnectAndX(sess, h, req)
	case protocol.CommandTreeConnect:
		return s.handleTreeConnect(sess, h, req)
	case protocol.CommandTreeDisconnect:
		return s.handleTreeDisconnect(sess, h, req)
	case protocol.CommandLogoffAndX:
		return s.handleLogoff(sess, h, req)
	case protocol.CommandEcho:
		return s.handleEcho(h, req)
	default:
		// A recognised-but-unimplemented command (the FS engine lands later) is
		// answered with an error so the client gets a definite reply rather than
		// hanging; an unknown command is likewise refused.
		return buildErrorResponse(h, req, statusNotSupported)
	}
}
