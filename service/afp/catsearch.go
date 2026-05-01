//go:build afp || all

package afp

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"strings"

	"github.com/pgodw/omnitalk/netlog"
)

// catSearchMaxDataLen is the maximum bytes of ResultsRecord data per reply.
// Based on one ATP packet: ATPMaxData(578) minus the 21-byte ASP/AFP reply header.
const catSearchMaxDataLen = 500 //557

func (s *Service) handleCatSearch(req *FPCatSearchReq) (*FPCatSearchRes, int32) {
	if req.ReqMatches <= 0 {
		return &FPCatSearchRes{}, ErrParamErr
	}
	volumeRoot, ok := s.volumeRootByID(req.VolumeID)
	if !ok {
		return &FPCatSearchRes{}, ErrParamErr
	}
	searchFS := s.fsForVolume(req.VolumeID)
	if searchFS == nil || !searchFS.Capabilities().CatSearch {
		return &FPCatSearchRes{}, ErrCallNotSupported
	}
	query := strings.TrimSpace(req.SearchQuery())
	netlog.Info("[AFP][CatSearch] volume=%d reqMatches=%d reqBitmap=0x%08x paramsLen=%d query=%q", req.VolumeID, req.ReqMatches, req.ReqBitmap, len(req.Parameters), query)
	if query == "" {
		return &FPCatSearchRes{}, ErrParamErr
	}
	paths, nextCursor, errCode := searchFS.CatSearch(volumeRoot, query, req.ReqMatches, req.CatalogPosition)
	if errCode != NoErr {
		return &FPCatSearchRes{}, errCode
	}

	fileBitmap := req.FileRsltBitmap
	dirBitmap := req.DirectoryRsltBitmap
	if fileBitmap == 0 && dirBitmap == 0 {
		dirBitmap = DirBitmapLongName | DirBitmapDirID | DirBitmapParentDID
	}

	// Decode the incoming cursor to know our starting offset in the backend cache.
	incomingOffset := binary.BigEndian.Uint32(req.CatalogPosition[4:8])

	data := new(bytes.Buffer)
	actCount := int32(0)
	pathsConsumed := 0

	for i, absPath := range paths {
		if actCount >= req.ReqMatches {
			pathsConsumed = i
			break
		}
		info, err := searchFS.Stat(absPath)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			continue
		}

		entryBuf := new(bytes.Buffer)
		entryBuf.WriteByte(0)
		entryBuf.WriteByte(0x80)
		parent := filepath.Dir(absPath)
		name := filepath.Base(absPath)
		s.packFileInfo(entryBuf, req.VolumeID, dirBitmap, parent, name, info, true)
		entry := entryBuf.Bytes()
		if len(entry)%2 != 0 {
			entryBuf.WriteByte(0)
			entry = entryBuf.Bytes()
		}
		// Per AFP CatSearch ResultsRecord format, StructLength excludes
		// the StructLength byte itself and the FileDir byte.
		entry[0] = byte(len(entry) - 2)

		if data.Len()+len(entry) > catSearchMaxDataLen {
			netlog.Debug("[AFP][CatSearch] stopping at payload cap: entries=%d dataLen=%d nextEntry=%d cap=%d", actCount, data.Len(), len(entry), catSearchMaxDataLen)
			pathsConsumed = i
			break
		}

		data.Write(entry)
		actCount++
		pathsConsumed = i + 1
	}

	// Determine the reply cursor.
	// If we stopped early due to payload cap, synthesize a continuation cursor so the
	// client resumes from the correct offset rather than re-starting the search.
	replyCursor := nextCursor
	if pathsConsumed < len(paths) {
		replyCursor = [16]byte{}
		replyCursor[0] = 0x01 // continuation flag
		// Carry the query hash from the backend cursor (bytes 1-3).
		replyCursor[1] = nextCursor[1]
		replyCursor[2] = nextCursor[2]
		replyCursor[3] = nextCursor[3]
		nextOffset := incomingOffset + uint32(pathsConsumed)
		replyCursor[4] = byte(nextOffset >> 24)
		replyCursor[5] = byte(nextOffset >> 16)
		replyCursor[6] = byte(nextOffset >> 8)
		replyCursor[7] = byte(nextOffset)
		netlog.Debug("[AFP][CatSearch] payload cap: synthesized continuation cursor offset=%d", nextOffset)
	}

	res := &FPCatSearchRes{
		CatalogPosition:     replyCursor,
		FileRsltBitmap:      fileBitmap,
		DirectoryRsltBitmap: dirBitmap,
		ActualCount:         actCount,
		Data:                data.Bytes(),
	}

	// Per AFP spec (matching Netatalk): return ErrEOFErr when this is the last page
	// (no more results to follow). Return NoErr only when more pages follow.
	if actCount == 0 || replyCursor[0] != 0x01 {
		netlog.Debug("[AFP][CatSearch] returning %d results (last page)", actCount)
		return res, ErrEOFErr
	}
	netlog.Debug("[AFP][CatSearch] returning %d results with cursor continuation=true offset=%d", actCount,
		binary.BigEndian.Uint32(replyCursor[4:8]))
	return res, NoErr
}
