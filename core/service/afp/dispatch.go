package afp

import (
	"strconv"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// AFP command codes (Inside Macintosh: Networking, AFP 2.x §6 "AFP command
// summary"). Only the spine's starter set is enumerated; further commands land
// in follow-up slices.
const (
	cmdByteRangeLock   uint8 = 1  // FPByteRangeLock
	cmdCopyFile        uint8 = 5  // FPCopyFile
	cmdGetDirParms     uint8 = 12 // FPGetDirParms
	cmdGetFileParms    uint8 = 13 // FPGetFileParms
	cmdMoveAndRename   uint8 = 23 // FPMoveAndRename
	cmdSetVolParms     uint8 = 32 // FPSetVolParms
	cmdExchangeFiles   uint8 = 42 // FPExchangeFiles
	cmdCloseDir        uint8 = 3  // FPCloseDir
	cmdCloseFork       uint8 = 4  // FPCloseFork
	cmdCloseVol        uint8 = 2  // FPCloseVol
	cmdCreateDir       uint8 = 6  // FPCreateDir
	cmdCreateFile      uint8 = 7  // FPCreateFile
	cmdDelete          uint8 = 8  // FPDelete
	cmdEnumerate       uint8 = 9  // FPEnumerate
	cmdFlush           uint8 = 10 // FPFlush
	cmdFlushFork       uint8 = 11 // FPFlushFork
	cmdGetForkParms    uint8 = 14 // FPGetForkParms
	cmdSetForkParms    uint8 = 31 // FPSetForkParms
	cmdGetSrvrInfo     uint8 = 15 // FPGetSrvrInfo (also served via ASPGetStatus)
	cmdGetSrvrParms    uint8 = 16 // FPGetSrvrParms
	cmdGetVolParms     uint8 = 17 // FPGetVolParms
	cmdLogin           uint8 = 18 // FPLogin
	cmdLoginCont       uint8 = 19 // FPLoginCont
	cmdLogout          uint8 = 20 // FPLogout
	cmdMapID           uint8 = 21 // FPMapID
	cmdMapName         uint8 = 22 // FPMapName
	cmdGetSrvrMsg      uint8 = 38 // FPGetSrvrMsg
	cmdSetDirParms     uint8 = 29 // FPSetDirParms
	cmdSetFileParms    uint8 = 30 // FPSetFileParms
	cmdOpenDir         uint8 = 25 // FPOpenDir
	cmdOpenFork        uint8 = 26 // FPOpenFork
	cmdOpenVol         uint8 = 24 // FPOpenVol
	cmdRead            uint8 = 27 // FPRead
	cmdRename          uint8 = 28 // FPRename
	cmdGetFileDirParms uint8 = 34 // FPGetFileDirParms
	cmdSetFileDirParms uint8 = 35 // FPSetFileDirParms
	cmdWrite           uint8 = 33 // FPWrite
)

// afpCommandName maps an AFP command byte to its FP name for debug logging; an
// unrecognised code renders as "FP#<n>" so the raw byte is still visible.
func afpCommandName(cmd uint8) string {
	switch cmd {
	case cmdByteRangeLock:
		return "FPByteRangeLock"
	case cmdCopyFile:
		return "FPCopyFile"
	case cmdGetDirParms:
		return "FPGetDirParms"
	case cmdGetFileParms:
		return "FPGetFileParms"
	case cmdMoveAndRename:
		return "FPMoveAndRename"
	case cmdSetVolParms:
		return "FPSetVolParms"
	case cmdExchangeFiles:
		return "FPExchangeFiles"
	case cmdCloseDir:
		return "FPCloseDir"
	case cmdCloseFork:
		return "FPCloseFork"
	case cmdCloseVol:
		return "FPCloseVol"
	case cmdCreateDir:
		return "FPCreateDir"
	case cmdCreateFile:
		return "FPCreateFile"
	case cmdDelete:
		return "FPDelete"
	case cmdEnumerate:
		return "FPEnumerate"
	case cmdFlush:
		return "FPFlush"
	case cmdFlushFork:
		return "FPFlushFork"
	case cmdGetForkParms:
		return "FPGetForkParms"
	case cmdSetForkParms:
		return "FPSetForkParms"
	case cmdGetSrvrInfo:
		return "FPGetSrvrInfo"
	case cmdGetSrvrParms:
		return "FPGetSrvrParms"
	case cmdGetVolParms:
		return "FPGetVolParms"
	case cmdLogin:
		return "FPLogin"
	case cmdLoginCont:
		return "FPLoginCont"
	case cmdLogout:
		return "FPLogout"
	case cmdMapID:
		return "FPMapID"
	case cmdMapName:
		return "FPMapName"
	case cmdGetSrvrMsg:
		return "FPGetSrvrMsg"
	case cmdSetDirParms:
		return "FPSetDirParms"
	case cmdSetFileParms:
		return "FPSetFileParms"
	case cmdOpenDir:
		return "FPOpenDir"
	case cmdOpenFork:
		return "FPOpenFork"
	case cmdOpenVol:
		return "FPOpenVol"
	case cmdRead:
		return "FPRead"
	case cmdRename:
		return "FPRename"
	case cmdGetFileDirParms:
		return "FPGetFileDirParms"
	case cmdSetFileDirParms:
		return "FPSetFileDirParms"
	case cmdWrite:
		return "FPWrite"
	case cmdOpenDT:
		return "FPOpenDT"
	case cmdCloseDT:
		return "FPCloseDT"
	case cmdAddComment:
		return "FPAddComment"
	case cmdRemoveComment:
		return "FPRemoveComment"
	case cmdGetComment:
		return "FPGetComment"
	case cmdAddIcon:
		return "FPAddIcon"
	case cmdGetIcon:
		return "FPGetIcon"
	case cmdGetIconInfo:
		return "FPGetIconInfo"
	case cmdAddAPPL:
		return "FPAddAPPL"
	case cmdRemoveAPPL:
		return "FPRemoveAPPL"
	case cmdGetAPPL:
		return "FPGetAPPL"
	case cmdCatSearch:
		return "FPCatSearch"
	default:
		return "FP#" + strconv.Itoa(int(cmd))
	}
}

// AFP result codes (kFP*; Inside Macintosh: Networking, "AFP result codes"). The
// wire form is a signed 32-bit OSErr carried in the ASP/ATP reply UserData.
const (
	afpNoErr            int32 = 0
	afpErrAccessDenied  int32 = -5000 // kFPAccessDenied
	afpErrCantMove      int32 = -5005 // kFPCantMove
	afpErrBadUAM        int32 = -5002 // kFPBadUAM
	afpErrBadVersNum    int32 = -5003 // kFPBadVersNum
	afpErrBitmapErr     int32 = -5004 // kFPBitmapErr (no/invalid bit set in a parameter bitmap)
	afpErrDiskFull      int32 = -5008 // kFPDiskFull
	afpErrEOFErr        int32 = -5009 // kFPEOFErr (read/write past end of fork)
	afpErrLockErr       int32 = -5013 // kFPLockErr (range locked by another fork)
	afpErrMiscErr       int32 = -5014 // kFPMiscErr
	afpErrNoMoreLocks   int32 = -5015 // kFPNoMoreLocks (lock table full)
	afpErrRangeNotLockd int32 = -5020 // kFPRangeNotLocked (unlock of an unheld range)
	afpErrRangeOverlap  int32 = -5021 // kFPRangeOverlap (range overlaps a lock this fork holds)
	afpErrObjectExists  int32 = -5017 // kFPObjectExists
	afpErrObjectNotFnd  int32 = -5018 // kFPObjectNotFound
	afpErrParamErr      int32 = -5019 // kFPParamErr
	afpErrCallNotSuppt  int32 = -5024 // kFPCallNotSupported
	afpErrObjectTypeErr int32 = -5025 // kFPObjectTypeErr
	afpErrDirNotFound   int32 = -5029 // kFPDirNotFound
	afpErrUserNotAuth   int32 = -5023 // kFPUserNotAuth (bad password / not authorised)
)

// afpSession is the per-ASP-session AFP state: whether the client has logged in,
// the volumes it has opened (volume id → bound Volume), and the forks it has open
// (fork ref → handle). It holds no socket or transport knowledge — that is the
// ASP layer's concern.
type afpSession struct {
	loggedIn bool
	// user is the authenticated identity resolved at FPLogin. Empty means a guest
	// login (No User Authent, or cleartext with no user store wired). It gates
	// which volumes the session may enumerate (FPGetSrvrParms) and open (FPOpenVol).
	user     string
	openVols map[uint16]*Volume
	forks    *forkTable
	dt       *dtTable // Desktop reference numbers handed out by FPOpenDT

	// idMu guards the fields other goroutines touch: the login identity snapshot
	// (Service.Sessions on the management plane) and the pending server message
	// (set by SendMessage/Disconnect/Stop, read by FPGetSrvrMsg). The dispatch
	// goroutine may keep reading loggedIn/user directly — every write goes through
	// setLogin on that same goroutine, so only cross-goroutine readers need the lock.
	idMu sync.Mutex
	// serverMsg is the pending server (operator) message a client fetches with
	// FPGetSrvrMsg type 1 after an SPAttention carrying the AspAttnMsg bit. It is
	// kept (not cleared) on read — an observed AppleShare server re-serves the
	// same text on every fetch — and the latest set wins.
	serverMsg string
}

func newAFPSession() *afpSession {
	return &afpSession{openVols: make(map[uint16]*Volume), forks: newForkTable(), dt: newDTTable()}
}

// setLogin records the session's login identity under idMu so cross-goroutine
// readers (Service.Sessions) see a consistent snapshot.
func (a *afpSession) setLogin(user string, loggedIn bool) {
	a.idMu.Lock()
	a.user, a.loggedIn = user, loggedIn
	a.idMu.Unlock()
}

// identity snapshots the login state for cross-goroutine readers.
func (a *afpSession) identity() (user string, loggedIn bool) {
	a.idMu.Lock()
	defer a.idMu.Unlock()
	return a.user, a.loggedIn
}

// setServerMsg stores the pending server message (latest wins).
func (a *afpSession) setServerMsg(msg string) {
	a.idMu.Lock()
	a.serverMsg = msg
	a.idMu.Unlock()
}

// serverMessage returns the pending server message, if any.
func (a *afpSession) serverMessage() string {
	a.idMu.Lock()
	defer a.idMu.Unlock()
	return a.serverMsg
}

// dispatchAFP decodes one AFP command block, runs the matching handler against
// the new Volumes, and returns the AFP reply block plus the result code. An empty
// block, an unknown command, or a command issued before login (other than the
// login/info calls) is rejected with the spec result code rather than a panic, so
// one bad request cannot disturb the session.
//
// It operates on the transport-neutral afpSession (the per-circuit AFP state), NOT
// on an ASP *session — command dispatch carries no transport knowledge, so the same
// engine serves an ASP circuit or a future DSI circuit (the §3-bis split; see
// conn.go). The ASP layer reaches it through Conn.Command.
//
// The command byte is block[0]; AFP request arguments follow. Most decoders here
// keep the command byte (offsets match Inside Macintosh's "Request block" tables,
// which count from the command byte); FPLogin is the historical exception whose
// arguments are documented from byte 1, so its handler is passed block[1:].
func (s *Service) dispatchAFP(a *afpSession, block []byte) (reply []byte, result int32) {
	if len(block) == 0 {
		return nil, afpErrParamErr
	}
	cmd := block[0]

	// Per-command debug trace: which AFP command ran and what result code it
	// returned. This is the seam every request crosses, so one line here makes the
	// whole command stream visible at debug level (the class of "silent -5024"
	// regression that is otherwise only diagnosable from a packet capture).
	if s.logger != nil && s.logger.Enabled(log.Debug) {
		defer func() {
			s.logger.Log(log.Debug, "AFP command",
				log.Str("cmd", afpCommandName(cmd)),
				log.Int("code", int64(cmd)),
				log.Int("result", int64(result)))
		}()
	}

	switch cmd {
	case cmdByteRangeLock:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpByteRangeLock(a, block)
	case cmdGetSrvrInfo:
		return s.serverInfoBlock(), afpNoErr
	case cmdLogin:
		return s.afpLogin(a, block[1:])
	case cmdLoginCont:
		// Single-step guest/cleartext login completes in FPLogin; a continuation
		// without a pending multi-step UAM is a parameter error.
		return nil, afpErrParamErr
	case cmdLogout:
		a.setLogin("", false)
		return nil, afpNoErr
	case cmdGetSrvrParms:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpGetSrvrParms(a), afpNoErr
	case cmdOpenVol:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpOpenVol(a, block)
	case cmdCloseVol:
		return s.afpCloseVol(a, block)
	case cmdGetVolParms:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpGetVolParms(a, block)
	case cmdEnumerate:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpEnumerate(a, block)
	case cmdGetFileDirParms:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpGetFileDirParms(a, block)
	case cmdSetFileDirParms, cmdSetDirParms, cmdSetFileParms:
		// FPSetDirParms (29) / FPSetFileParms (30) share the unified
		// FPSetFileDirParms (35) request layout in this server.
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpSetFileDirParms(a, block)
	case cmdMapID:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpMapID(a, block)
	case cmdMapName:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpMapName(a, block)
	case cmdGetSrvrMsg:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpGetSrvrMsg(a, block)
	case cmdGetDirParms:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpGetDirParms(a, block)
	case cmdGetFileParms:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpGetFileParms(a, block)
	case cmdMoveAndRename:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpMoveAndRename(a, block)
	case cmdExchangeFiles:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpExchangeFiles(a, block)
	case cmdCopyFile:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpCopyFile(a, block)
	case cmdSetVolParms:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpSetVolParms(a, block)
	case cmdCreateFile:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpCreateFile(a, block)
	case cmdCreateDir:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpCreateDir(a, block)
	case cmdDelete:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpDelete(a, block)
	case cmdRename:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpRename(a, block)
	case cmdOpenDir:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpOpenDir(a, block)
	case cmdCloseDir:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpCloseDir(a, block)
	case cmdOpenFork:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpOpenFork(a, block)
	case cmdRead:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpRead(a, block)
	case cmdWrite:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpWrite(a, block)
	case cmdCloseFork:
		return s.afpCloseFork(a, block)
	case cmdFlush:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpFlush(a, block)
	case cmdFlushFork:
		return s.afpFlushFork(a, block)
	case cmdGetForkParms:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpGetForkParms(a, block)
	case cmdSetForkParms:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpSetForkParms(a, block)
	case cmdOpenDT:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpOpenDT(a, block)
	case cmdCloseDT:
		return s.afpCloseDT(a, block)
	case cmdAddComment:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpAddComment(a, block)
	case cmdRemoveComment:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpRemoveComment(a, block)
	case cmdGetComment:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpGetComment(a, block)
	case cmdAddIcon:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpAddIcon(a, block)
	case cmdGetIcon:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpGetIcon(a, block)
	case cmdGetIconInfo:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpGetIconInfo(a, block)
	case cmdAddAPPL:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpAddAPPL(a, block)
	case cmdRemoveAPPL:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpRemoveAPPL(a, block)
	case cmdGetAPPL:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpGetAPPL(a, block)
	case cmdCatSearch:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpCatSearch(a, block)
	default:
		return nil, afpErrCallNotSuppt
	}
}
