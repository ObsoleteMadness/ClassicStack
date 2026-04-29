//go:build afp || all

package afp

import (
	"runtime/debug"

	"github.com/pgodw/omnitalk/netlog"
)

// Request is the decoded form of an inbound AFP command.
type Request interface {
	Unmarshal(data []byte) error
	String() string
}

// Response is a Service-produced AFP reply ready for wire emission.
type Response interface {
	Marshal() []byte
	String() string
}

// HandleCommand decodes one AFP command, dispatches it through the registry,
// and returns the marshalled reply (or an AFP error code). Panics in handlers
// are recovered and surfaced as ErrParamErr so a single bad request cannot
// take down the session.
func (s *Service) HandleCommand(data []byte) (resBytes []byte, errCode int32) {
	defer func() {
		if r := recover(); r != nil {
			netlog.Warn("[AFP] PANIC in cmd=%d: %v\n%s", data[0], r, debug.Stack())
			resBytes = nil
			errCode = ErrParamErr
		}
	}()
	if len(data) == 0 {
		return nil, ErrParamErr
	}

	cmd := data[0]
	afpCommandsTotal.Inc()

	spec, ok := commandRegistry[cmd]
	if !ok {
		netlog.Debug("[AFP] unknown command %d", cmd)
		return nil, ErrCallNotSupported
	}

	req := spec.newReq()
	cmdData := data
	if spec.stripCmdByte {
		cmdData = data[1:]
	}

	if err := req.Unmarshal(cmdData); err != nil {
		netlog.Debug("[AFP] Error unmarshaling cmd %d: %v", cmd, err)
		return nil, ErrParamErr
	}

	s.logPacket("[AFP] → %s", req.String())
	s.logResolvedPaths(req)

	res, errCode := spec.handle(s, req)

	if res != nil {
		s.logPacket("[AFP] ← %s (err=%d)", res.String(), errCode)
		resBytes = res.Marshal()
	} else if errCode != NoErr {
		s.logPacket("[AFP] ← cmd=%d err=%d", cmd, errCode)
	}

	return resBytes, errCode
}

// commandSpec describes how to dispatch one AFP command code.
//
// Each command names a request constructor (so we can decode into the right
// struct), a handler bound to the running Service, and an optional flag that
// strips the leading command byte before Unmarshal — FPLogin is the lone
// command whose request decoder expects the command byte already removed.
type commandSpec struct {
	name         string
	newReq       func() Request
	handle       func(s *Service, req Request) (Response, int32)
	stripCmdByte bool
}

// commandRegistry maps AFP command codes to their dispatch specs.
//
// Adding a new command: declare the spec here. The dispatcher in
// HandleCommand handles unmarshal, logging, response packing, and panic
// recovery uniformly.
//
// Each handle closure mirrors the original switch's nil-response treatment:
// if the concrete handler returns a nil pointer, surface it as a nil Response
// so the dispatcher skips Marshal.
var commandRegistry = map[uint8]commandSpec{
	FPGetSrvrInfo: {
		name:   "FPGetSrvrInfo",
		newReq: func() Request { return &FPGetSrvrInfoReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleGetSrvrInfo(req.(*FPGetSrvrInfoReq))
			if err != nil {
				return nil, ErrMiscErr
			}
			return res, NoErr
		},
	},
	FPGetSrvrParms: {
		name:   "FPGetSrvrParms",
		newReq: func() Request { return &FPGetSrvrParmsReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleGetSrvrParms(req.(*FPGetSrvrParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPLogin: {
		name:         "FPLogin",
		newReq:       func() Request { return &FPLoginReq{} },
		stripCmdByte: true,
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleLogin(req.(*FPLoginReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPLogout: {
		name:   "FPLogout",
		newReq: func() Request { return &FPLogoutReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleLogout(req.(*FPLogoutReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPOpenVol: {
		name:   "FPOpenVol",
		newReq: func() Request { return &FPOpenVolReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleOpenVol(req.(*FPOpenVolReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPGetVolParms: {
		name:   "FPGetVolParms",
		newReq: func() Request { return &FPGetVolParmsReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleGetVolParms(req.(*FPGetVolParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPOpenDir: {
		name:   "FPOpenDir",
		newReq: func() Request { return &FPOpenDirReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleOpenDir(req.(*FPOpenDirReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPCloseVol: {
		name:   "FPCloseVol",
		newReq: func() Request { return &FPCloseVolReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleCloseVol(req.(*FPCloseVolReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPCloseDir: {
		name:   "FPCloseDir",
		newReq: func() Request { return &FPCloseDirReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleCloseDir(req.(*FPCloseDirReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPCloseFork: {
		name:   "FPCloseFork",
		newReq: func() Request { return &FPCloseForkReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleCloseFork(req.(*FPCloseForkReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPFlush: {
		name:   "FPFlush",
		newReq: func() Request { return &FPFlushReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			return s.handleFlush(req.(*FPFlushReq))
		},
	},
	FPFlushFork: {
		name:   "FPFlushFork",
		newReq: func() Request { return &FPFlushForkReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			return s.handleFlushFork(req.(*FPFlushForkReq))
		},
	},
	FPEnumerate: {
		name:   "FPEnumerate",
		newReq: func() Request { return &FPEnumerateReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleEnumerate(req.(*FPEnumerateReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPGetFileDirParms: {
		name:   "FPGetFileDirParms",
		newReq: func() Request { return &FPGetFileDirParmsReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleGetFileDirParms(req.(*FPGetFileDirParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPOpenFork: {
		name:   "FPOpenFork",
		newReq: func() Request { return &FPOpenForkReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleOpenFork(req.(*FPOpenForkReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPRead: {
		name:   "FPRead",
		newReq: func() Request { return &FPReadReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleRead(req.(*FPReadReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPWrite: {
		name:   "FPWrite",
		newReq: func() Request { return &FPWriteReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleWrite(req.(*FPWriteReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPCreateFile: {
		name:   "FPCreateFile",
		newReq: func() Request { return &FPCreateFileReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleCreateFile(req.(*FPCreateFileReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPCreateDir: {
		name:   "FPCreateDir",
		newReq: func() Request { return &FPCreateDirReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleCreateDir(req.(*FPCreateDirReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPDelete: {
		name:   "FPDelete",
		newReq: func() Request { return &FPDeleteReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleDelete(req.(*FPDeleteReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPRename: {
		name:   "FPRename",
		newReq: func() Request { return &FPRenameReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleRename(req.(*FPRenameReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPByteRangeLock: {
		name:   "FPByteRangeLock",
		newReq: func() Request { return &FPByteRangeLockReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleByteRangeLock(req.(*FPByteRangeLockReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPCopyFile: {
		name:   "FPCopyFile",
		newReq: func() Request { return &FPCopyFileReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleCopyFile(req.(*FPCopyFileReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPGetDirParms: {
		name:   "FPGetDirParms",
		newReq: func() Request { return &FPGetDirParmsReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleGetDirParms(req.(*FPGetDirParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPGetFileParms: {
		name:   "FPGetFileParms",
		newReq: func() Request { return &FPGetFileParmsReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleGetFileParms(req.(*FPGetFileParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPGetForkParms: {
		name:   "FPGetForkParms",
		newReq: func() Request { return &FPGetForkParmsReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleGetForkParms(req.(*FPGetForkParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPLoginCont: {
		name:   "FPLoginCont",
		newReq: func() Request { return &FPLoginContReq{} },
		// TODO: Implement second-phase UAM login (AFP 2.x §5.1.19).
		handle: func(s *Service, req Request) (Response, int32) {
			return nil, ErrCallNotSupported
		},
	},
	FPMapID: {
		name:   "FPMapID",
		newReq: func() Request { return &FPMapIDReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleMapID(req.(*FPMapIDReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPMapName: {
		name:   "FPMapName",
		newReq: func() Request { return &FPMapNameReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleMapName(req.(*FPMapNameReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPMoveAndRename: {
		name:   "FPMoveAndRename",
		newReq: func() Request { return &FPMoveAndRenameReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleMoveAndRename(req.(*FPMoveAndRenameReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPSetDirParms: {
		name:   "FPSetDirParms",
		newReq: func() Request { return &FPSetDirParmsReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleSetDirParms(req.(*FPSetDirParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPSetFileParms: {
		name:   "FPSetFileParms",
		newReq: func() Request { return &FPSetFileParmsReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleSetFileParms(req.(*FPSetFileParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPSetForkParms: {
		name:   "FPSetForkParms",
		newReq: func() Request { return &FPSetForkParmsReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleSetForkParms(req.(*FPSetForkParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPSetVolParms: {
		name:   "FPSetVolParms",
		newReq: func() Request { return &FPSetVolParmsReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleSetVolParms(req.(*FPSetVolParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPSetFileDirParms: {
		name:   "FPSetFileDirParms",
		newReq: func() Request { return &FPSetFileDirParmsReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleSetFileDirParms(req.(*FPSetFileDirParmsReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPExchangeFiles: {
		name:   "FPExchangeFiles",
		newReq: func() Request { return &FPExchangeFilesReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleExchangeFiles(req.(*FPExchangeFilesReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPGetSrvrMsg: {
		name:   "FPGetSrvrMsg",
		newReq: func() Request { return &FPGetSrvrMsgReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			r := req.(*FPGetSrvrMsgReq)
			return &FPGetSrvrMsgRes{MessageType: r.MessageType}, NoErr
		},
	},
	FPChangePassword: {
		name:   "FPChangePassword",
		newReq: func() Request { return &FPUnsupportedReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			return nil, ErrCallNotSupported
		},
	},
	FPGetUserInfo: {
		name:   "FPGetUserInfo",
		newReq: func() Request { return &FPUnsupportedReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			return nil, ErrCallNotSupported
		},
	},
	FPCatSearch: {
		name:   "FPCatSearch",
		newReq: func() Request { return &FPCatSearchReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleCatSearch(req.(*FPCatSearchReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPOpenDT: {
		name:   "FPOpenDT",
		newReq: func() Request { return &FPOpenDTReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleOpenDT(req.(*FPOpenDTReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPCloseDT: {
		name:   "FPCloseDT",
		newReq: func() Request { return &FPCloseDTReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleCloseDT(req.(*FPCloseDTReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPGetIcon: {
		name:   "FPGetIcon",
		newReq: func() Request { return &FPGetIconReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleGetIcon(req.(*FPGetIconReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPGetIconInfo: {
		name:   "FPGetIconInfo",
		newReq: func() Request { return &FPGetIconInfoReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleGetIconInfo(req.(*FPGetIconInfoReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPAddIcon: {
		name:   "FPAddIcon",
		newReq: func() Request { return &FPAddIconReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleAddIcon(req.(*FPAddIconReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPAddAPPL: {
		name:   "FPAddAPPL",
		newReq: func() Request { return &FPAddAPPLReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleAddAPPL(req.(*FPAddAPPLReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPRemoveAPPL: {
		name:   "FPRemoveAPPL",
		newReq: func() Request { return &FPRemoveAPPLReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleRemoveAPPL(req.(*FPRemoveAPPLReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPGetAPPL: {
		name:   "FPGetAPPL",
		newReq: func() Request { return &FPGetAPPLReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleGetAPPL(req.(*FPGetAPPLReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPAddComment: {
		name:   "FPAddComment",
		newReq: func() Request { return &FPAddCommentReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleAddComment(req.(*FPAddCommentReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPRemoveComment: {
		name:   "FPRemoveComment",
		newReq: func() Request { return &FPRemoveCommentReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleRemoveComment(req.(*FPRemoveCommentReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
	FPGetComment: {
		name:   "FPGetComment",
		newReq: func() Request { return &FPGetCommentReq{} },
		handle: func(s *Service, req Request) (Response, int32) {
			res, err := s.handleGetComment(req.(*FPGetCommentReq))
			if res == nil {
				return nil, err
			}
			return res, err
		},
	},
}
