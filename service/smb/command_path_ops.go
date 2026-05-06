package smb

import (
	"bytes"
	"encoding/binary"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/pkg/vfs"
)

const (
	smbStatusAccessDenied    = 0xC0000022
	smbStatusNameNotFound    = 0xC000007F
	smbStatusFileIsDirectory = 0xC00000BA
	smbStatusNotADirectory   = 0xC0000103
)

func (s *Service) handleDelete(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+3 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	_, slot, fsys, ok := s.resolveRequestTree(req, conn)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}
	if s.shares[slot.shareIdx].ReadOnly {
		return buildSMBErrorResponse(req, smbStatusAccessDenied)
	}

	path, ok := parseSMBPath(req)
	if !ok || path == "" {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}
	if strings.Contains(path, "*") || strings.Contains(path, "?") {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	info, err := fsys.Stat(path)
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}
	if info.IsDir() {
		return buildSMBErrorResponse(req, smbStatusFileIsDirectory)
	}

	if err := fsys.Remove(path); err != nil {
		return buildSMBErrorResponse(req, smbStatusAccessDenied)
	}
	return buildSimpleSuccessResponse(req)
}

func (s *Service) handleRename(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+3 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	_, slot, fsys, ok := s.resolveRequestTree(req, conn)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}
	if s.shares[slot.shareIdx].ReadOnly {
		return buildSMBErrorResponse(req, smbStatusAccessDenied)
	}

	oldPath, newPath, ok := parseRenamePaths(req)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}
	if strings.Contains(oldPath, "*") || strings.Contains(oldPath, "?") || strings.Contains(newPath, "*") || strings.Contains(newPath, "?") {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	if _, err := fsys.Stat(oldPath); err != nil {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}
	if err := fsys.Rename(oldPath, newPath); err != nil {
		return buildSMBErrorResponse(req, smbStatusAccessDenied)
	}
	return buildSimpleSuccessResponse(req)
}

func (s *Service) handleDeleteDirectory(req []byte, conn *connState) []byte {
	if len(req) < smbHeaderLen+3 {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	_, slot, fsys, ok := s.resolveRequestTree(req, conn)
	if !ok {
		return buildSMBErrorResponse(req, smbStatusBadTID)
	}
	if s.shares[slot.shareIdx].ReadOnly {
		return buildSMBErrorResponse(req, smbStatusAccessDenied)
	}

	path, ok := parseSMBPath(req)
	if !ok || path == "" {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}
	if strings.Contains(path, "*") || strings.Contains(path, "?") {
		return buildSMBErrorResponse(req, smbStatusNotSupported)
	}

	info, err := fsys.Stat(path)
	if err != nil {
		return buildSMBErrorResponse(req, smbStatusNameNotFound)
	}
	if !info.IsDir() {
		return buildSMBErrorResponse(req, smbStatusNotADirectory)
	}

	if err := fsys.Remove(path); err != nil {
		return buildSMBErrorResponse(req, smbStatusAccessDenied)
	}
	return buildSimpleSuccessResponse(req)
}

func parseRenamePaths(req []byte) (oldPath, newPath string, ok bool) {
	bytesArea, ok := smbBytesArea(req)
	if !ok || len(bytesArea) == 0 {
		return "", "", false
	}

	parts := make([]string, 0, 2)
	buf := bytesArea
	for len(buf) > 0 && len(parts) < 2 {
		if buf[0] == 0x04 {
			buf = buf[1:]
		}
		nul := bytes.IndexByte(buf, 0)
		if nul < 0 {
			break
		}
		part := strings.TrimLeft(strings.TrimSpace(string(buf[:nul])), "\\")
		if part != "" {
			parts = append(parts, part)
		}
		buf = buf[nul+1:]
	}

	if len(parts) < 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (s *Service) resolveRequestTree(req []byte, conn *connState) (tid uint16, slot treeSlot, fsys vfs.FileSystem, ok bool) {
	if len(req) < smbHeaderLen {
		return 0, treeSlot{}, nil, false
	}
	tid = binary.LittleEndian.Uint16(req[smbOffTID : smbOffTID+2])

	conn.mu.Lock()
	slot, ok = conn.tids[tid]
	conn.mu.Unlock()
	if !ok {
		return 0, treeSlot{}, nil, false
	}

	s.mu.Lock()
	fsys, ok = s.shareFSes[slot.shareIdx]
	s.mu.Unlock()
	if !ok || fsys == nil {
		return 0, treeSlot{}, nil, false
	}

	return tid, slot, fsys, true
}
