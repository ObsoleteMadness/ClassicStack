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
// The spine handles the session-establishment commands — NEGOTIATE,
// SESSION_SETUP_ANDX, TREE_CONNECT(_ANDX), TREE_DISCONNECT, LOGOFF_ANDX, ECHO —
// and the FS command engine (this slice) handles the file/path/find commands over
// the bound *Share's FS: OPEN[_ANDX]/CREATE, READ[_ANDX]/WRITE[_ANDX],
// CLOSE/FLUSH, DELETE/RENAME, CREATE_DIRECTORY/DELETE_DIRECTORY/CHECK_DIRECTORY,
// QUERY_INFORMATION[_DISK], NT_CREATE_ANDX (the NT/2000/XP open-or-create path,
// files and directories), and the TRANS2 FIND_FIRST2/FIND_NEXT2/FIND_CLOSE2 +
// QUERY_PATH/FILE_INFO subcommands. TRANSACTION over the IPC$ \PIPE\LANMAN pipe
// serves the RAP NetServerEnum2 ("get server list", from the browser via the
// BrowseProvider seam — the one place SMB meets the datagram-layer browser) and
// NetShareEnum ("get share list", from SMB's own bound shares + IPC$) — lanman.go.
// A recognised-but-unimplemented command (the byte-range
// LOCKING_ANDX / MPX / raw-read-write paths, out of M7 scope) answers
// STATUS_NOT_SUPPORTED so the client gets a definite reply; an unparseable frame
// is dropped (nil) so a malformed packet cannot wedge the connection.
//
// When the primary command is an AndX command carrying chained secondaries
// ([smb6.0] 988 "ANDX SMB Messages" — e.g. NT's SESSION_SETUP_ANDX →
// TREE_CONNECT_ANDX), the chained commands are dispatched in turn and their
// response blocks spliced into the single reply (andx.go).
func (s *Service) Dispatch(sess *smbSession, req []byte) []byte {
	h, err := protocol.DecodeHeader(req)
	if err != nil {
		return nil // not an SMB frame — drop it
	}
	resp := s.dispatchOne(sess, h, req)
	if resp == nil || !isAndXRequest(h.Command) {
		return resp
	}
	return s.processAndXChain(sess, h, req, resp)
}

// dispatchOne serves a single command block — the primary command of a message,
// or one chained block re-framed by processAndXChain. h.Command selects the
// handler (the header bytes in req are not re-decoded).
func (s *Service) dispatchOne(sess *smbSession, h protocol.Header, req []byte) []byte {
	switch h.Command {
	case protocol.CommandNegotiate:
		return s.handleNegotiate(sess, h, req)
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

	// --- FS command engine: file I/O ---
	case protocol.CommandOpenAndX:
		return s.handleOpenAndX(sess, h, req)
	case protocol.CommandOpen:
		return s.handleOpen(sess, h, req)
	case protocol.CommandCreate:
		return s.handleCreate(sess, h, req)
	case protocol.CommandReadAndX:
		return s.handleReadAndX(sess, h, req)
	case protocol.CommandRead:
		return s.handleRead(sess, h, req)
	case protocol.CommandWriteAndX:
		return s.handleWriteAndX(sess, h, req)
	case protocol.CommandWrite:
		return s.handleWrite(sess, h, req)
	case protocol.CommandClose:
		return s.handleClose(sess, h, req)
	case protocol.CommandFlush:
		return s.handleFlush(sess, h, req)
	case protocol.CommandSeek:
		return s.handleSeek(sess, h, req)

	// --- multiplexed / raw transfer: WRITE_MPX served; READ_MPX / WRITE_RAW
	// answer the fall-back forms that steer the client to plain READ / WRITE ---
	case protocol.CommandWriteMPX:
		return s.handleWriteMPX(sess, h, req)
	case protocol.CommandReadMPX:
		return s.handleReadMPX(sess, h, req)
	case protocol.CommandWriteRaw:
		return s.handleWriteRaw(sess, h, req)

	// --- byte-range locking (Excel / Access / DOS databases) ---
	case protocol.CommandLockingAndX:
		return s.handleLockingAndX(sess, h, req)

	// --- FS command engine: path operations ---
	case protocol.CommandDelete:
		return s.handleDelete(sess, h, req)
	case protocol.CommandRename:
		return s.handleRename(sess, h, req)
	case protocol.CommandCreateDirectory:
		return s.handleCreateDirectory(sess, h, req)
	case protocol.CommandDeleteDirectory:
		return s.handleDeleteDirectory(sess, h, req)
	case protocol.CommandCheckDirectory:
		return s.handleCheckDirectory(sess, h, req)
	case protocol.CommandQueryInformation:
		return s.handleQueryInformation(sess, h, req)
	case protocol.CommandQueryInformationDisk:
		return s.handleQueryInformationDisk(sess, h, req)

	// --- CORE-dialect directory browse (MS-DOS LAN Manager / WfW 3.11) ---
	case protocol.CommandSearch:
		return s.handleSearch(sess, h, req)

	case protocol.CommandNtCreateAndX:
		return s.handleNtCreateAndX(sess, h, req)

	// --- NT_TRANSACT: only NOTIFY_CHANGE is served (the §10d async-completion path) ---
	case protocol.CommandNtTransact:
		return s.handleNtTransact(sess, h, req)

	// --- IPC$ named-pipe RAP: NetServerEnum2 (browse list) over \PIPE\LANMAN ---
	case protocol.CommandTransaction:
		return s.handleTransaction(sess, h, req)

	// --- FS command engine: TRANS2 find/query ---
	case protocol.CommandTransaction2:
		return s.handleTransaction2(sess, h, req)
	case protocol.CommandFindClose2:
		return s.handleFindClose2(sess, h, req)

	default:
		return buildErrorResponse(h, req, statusNotSupported)
	}
}
