//go:build afp || all

package afp

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
)

func (s *Service) logPacket(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if s.dumper != nil {
		s.dumper.LogPacket(msg)
	}
}

func (s *Service) logResolvedPaths(req Request) {
	switch r := req.(type) {
	case *FPOpenDirReq:
		s.logResolvedPath("FPOpenDir", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPEnumerateReq:
		s.logResolvedPath("FPEnumerate", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPGetFileDirParmsReq:
		s.logResolvedPath("FPGetFileDirParms", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPGetDirParmsReq:
		s.logResolvedPath("FPGetDirParms", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPGetFileParmsReq:
		s.logResolvedPath("FPGetFileParms", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPOpenForkReq:
		s.logResolvedPath("FPOpenFork", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPCreateFileReq:
		s.logResolvedPath("FPCreateFile", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPCreateDirReq:
		s.logResolvedPath("FPCreateDir", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPDeleteReq:
		s.logResolvedPath("FPDelete", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPSetDirParmsReq:
		s.logResolvedPath("FPSetDirParms", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPSetFileParmsReq:
		s.logResolvedPath("FPSetFileParms", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPSetFileDirParmsReq:
		s.logResolvedPath("FPSetFileDirParms", r.VolumeID, r.DirID, r.PathType, r.Path)
	case *FPRenameReq:
		s.logResolvedPath("FPRename old", r.VolumeID, r.DirID, r.PathType, r.Name)
		s.logResolvedPath("FPRename new", r.VolumeID, r.DirID, r.NewPathType, r.NewName)
	case *FPMoveAndRenameReq:
		s.logResolvedPath("FPMoveAndRename src", r.VolumeID, r.SrcDirID, r.SrcPathType, r.SrcName)
		s.logResolvedPath("FPMoveAndRename dstDir", r.VolumeID, r.DstDirID, r.DstPathType, r.DstDirName)
	case *FPExchangeFilesReq:
		s.logResolvedPath("FPExchangeFiles src", r.VolumeID, r.SrcDirID, r.SrcPathType, r.SrcName)
		s.logResolvedPath("FPExchangeFiles dst", r.VolumeID, r.DstDirID, r.DstPathType, r.DstName)
	case *FPCopyFileReq:
		s.logResolvedPath("FPCopyFile src", r.SrcVolumeID, r.SrcDirID, r.SrcPathType, r.SrcName)
		s.logResolvedPath("FPCopyFile dstDir", r.DstVolumeID, r.DstDirID, r.DstPathType, r.DstDirName)
	case *FPAddAPPLReq:
		s.logResolvedPathFromDTRef("FPAddAPPL", r.DTRefNum, r.DirID, r.PathType, r.Path)
	case *FPRemoveAPPLReq:
		s.logResolvedPathFromDTRef("FPRemoveAPPL", r.DTRefNum, r.DirID, r.PathType, r.Path)
	case *FPAddCommentReq:
		s.logResolvedPathFromDTRef("FPAddComment", r.DTRefNum, r.DirID, r.PathType, r.Path)
	case *FPRemoveCommentReq:
		s.logResolvedPathFromDTRef("FPRemoveComment", r.DTRefNum, r.DirID, r.PathType, r.Path)
	case *FPGetCommentReq:
		s.logResolvedPathFromDTRef("FPGetComment", r.DTRefNum, r.DirID, r.PathType, r.Path)
	case *FPCatSearchReq:
		s.logResolvedPath("FPCatSearch", r.VolumeID, CNIDRoot, PathTypeLongNames, "")
	}
}

func (s *Service) logResolvedPath(op string, volumeID uint16, dirID uint32, pathType uint8, rawPath string) {
	resolved, errCode := s.resolveVolumePath(volumeID, dirID, rawPath, pathType)
	if errCode == NoErr {
		netlog.Debug("[AFP][Path] %s vol=%d dirID=%d pathType=%d raw=%q resolved=%q", op, volumeID, dirID, pathType, rawPath, resolved)
		return
	}
	netlog.Debug("[AFP][Path] %s vol=%d dirID=%d pathType=%d raw=%q unresolved err=%d", op, volumeID, dirID, pathType, rawPath, errCode)
}

func (s *Service) logResolvedPathFromDTRef(op string, dtRefNum uint16, dirID uint32, pathType uint8, rawPath string) {
	volID, ok := s.desktop.volumeOf(dtRefNum)
	if !ok {
		netlog.Debug("[AFP][Path] %s dtRef=%d dirID=%d pathType=%d raw=%q unresolved err=%d", op, dtRefNum, dirID, pathType, rawPath, ErrParamErr)
		return
	}
	s.logResolvedPath(op, volID, dirID, pathType, rawPath)
}
