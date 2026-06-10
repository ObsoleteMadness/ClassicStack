package afp

// AFP command codes (Inside Macintosh: Networking, AFP 2.x §6 "AFP command
// summary"). Only the spine's starter set is enumerated; further commands land
// in follow-up slices.
const (
	cmdCloseVol        uint8 = 2  // FPCloseVol
	cmdEnumerate       uint8 = 9  // FPEnumerate
	cmdGetSrvrInfo     uint8 = 15 // FPGetSrvrInfo (also served via ASPGetStatus)
	cmdGetSrvrParms    uint8 = 16 // FPGetSrvrParms
	cmdLogin           uint8 = 18 // FPLogin
	cmdLoginCont       uint8 = 19 // FPLoginCont
	cmdLogout          uint8 = 20 // FPLogout
	cmdOpenVol         uint8 = 24 // FPOpenVol
	cmdGetFileDirParms uint8 = 34 // FPGetFileDirParms
)

// AFP result codes (kFP*; Inside Macintosh: Networking, "AFP result codes"). The
// wire form is a signed 32-bit OSErr carried in the ASP/ATP reply UserData.
const (
	afpNoErr           int32 = 0
	afpErrAccessDenied int32 = -5000 // kFPAccessDenied
	afpErrBadUAM       int32 = -5002 // kFPBadUAM
	afpErrBadVersNum   int32 = -5003 // kFPBadVersNum
	afpErrMiscErr      int32 = -5014 // kFPMiscErr
	afpErrObjectNotFnd int32 = -5018 // kFPObjectNotFound
	afpErrParamErr     int32 = -5019 // kFPParamErr
	afpErrCallNotSuppt int32 = -5024 // kFPCallNotSupported
)

// afpSession is the per-ASP-session AFP state: whether the client has logged in,
// and the volumes it has opened (volume id → bound Volume). It holds no socket or
// transport knowledge — that is the ASP layer's concern.
type afpSession struct {
	loggedIn bool
	openVols map[uint16]*Volume
}

func newAFPSession() *afpSession {
	return &afpSession{openVols: make(map[uint16]*Volume)}
}

// dispatchAFP decodes one AFP command block, runs the matching handler against
// the new Volumes, and returns the AFP reply block plus the result code. An empty
// block, an unknown command, or a command issued before login (other than the
// login/info calls) is rejected with the spec result code rather than a panic, so
// one bad request cannot disturb the session.
//
// The command byte is block[0]; AFP request arguments follow. Most decoders here
// keep the command byte (offsets match Inside Macintosh's "Request block" tables,
// which count from the command byte); FPLogin is the historical exception whose
// arguments are documented from byte 1, so its handler is passed block[1:].
func (s *Service) dispatchAFP(sess *session, block []byte) (reply []byte, result int32) {
	if len(block) == 0 {
		return nil, afpErrParamErr
	}
	cmd := block[0]
	a := sess.afp

	switch cmd {
	case cmdGetSrvrInfo:
		return s.serverInfoBlock(), afpNoErr
	case cmdLogin:
		return s.afpLogin(a, block[1:])
	case cmdLoginCont:
		// Single-step guest/cleartext login completes in FPLogin; a continuation
		// without a pending multi-step UAM is a parameter error.
		return nil, afpErrParamErr
	case cmdLogout:
		a.loggedIn = false
		return nil, afpNoErr
	case cmdGetSrvrParms:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpGetSrvrParms(), afpNoErr
	case cmdOpenVol:
		if !a.loggedIn {
			return nil, afpErrAccessDenied
		}
		return s.afpOpenVol(a, block)
	case cmdCloseVol:
		return s.afpCloseVol(a, block)
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
	default:
		return nil, afpErrCallNotSuppt
	}
}
