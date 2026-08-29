package archive

import (
	"bytes"
	"errors"
	"io"
	"strings"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/StuffIt-Go/stuffit"
)

// isStuffIt reports whether data starts with a recognised StuffIt catalog
// signature: classic "SIT!" (StuffIt 1.5 through 5.5) or the StuffIt 5.x
// catalog's "StuffIt (c)1997-" banner. This is a cheap sniff for Sniff/Expand
// dispatch; stuffit.NewParser does the real, wrapper-aware detection
// (MacBinary/AppleSingle/BinHex payloads) once expandStuffIt actually parses.
func isStuffIt(data []byte) bool {
	return bytes.HasPrefix(data, []byte("SIT!")) || bytes.HasPrefix(data, []byte("StuffIt (c)1997-"))
}

// expandStuffIt expands a StuffIt archive — classic SIT! (1.5–5.5) or SIT5 —
// via github.com/ObsoleteMadness/StuffIt-Go, entirely in memory. See that
// package's doc comment for the full format/wrapper support matrix (it also
// transparently unwraps a MacBinary/AppleSingle/BinHex-wrapped archive, so a
// caller need not sniff those separately for StuffIt payloads).
func expandStuffIt(name string, data []byte, finderInfo [32]byte) ([]Node, error) {
	_ = name
	_ = finderInfo
	if !isStuffIt(data) {
		return nil, ErrUnsupportedFormat
	}

	p := stuffit.NewParser(bytes.NewReader(data), int64(len(data)))
	arc, err := p.Parse()
	if err != nil {
		return nil, ErrCorrupt
	}

	tb := newTreeBuilder()
	sink := &stuffitSink{tb: tb}
	if err := p.Extract(arc, sink, stuffit.ExtractOptions{}); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, ErrCorrupt
		}
		return nil, err
	}

	roots := tb.roots()
	if len(roots) == 0 {
		return nil, ErrCorrupt
	}
	return roots, nil
}

// stuffitSink adapts the shared treeBuilder to stuffit.ForkWriter +
// stuffit.DirectoryWriter, the two hooks Extract drives per entry.
type stuffitSink struct {
	tb *treeBuilder
}

var (
	_ stuffit.ForkWriter      = (*stuffitSink)(nil)
	_ stuffit.DirectoryWriter = (*stuffitSink)(nil)
)

func (s *stuffitSink) WriteDataFork(entry stuffit.Entry, r stuffit.ForkReader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.tb.setData(entryPath(entry), data)
	return nil
}

func (s *stuffitSink) WriteResourceFork(entry stuffit.Entry, r stuffit.ForkReader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.tb.setResource(entryPath(entry), data, entryFinderInfo(entry))
	return nil
}

func (s *stuffitSink) CreateDirectory(entry stuffit.Entry) error {
	s.tb.addDir(entryPath(entry))
	return nil
}

// entryPath normalises a stuffit.Entry's archive path to the '/'-separated,
// no-leading-slash form treeBuilder expects (matching zip.go's convention).
func entryPath(entry stuffit.Entry) string {
	p := strings.ReplaceAll(entry.Path, "\\", "/")
	return strings.TrimPrefix(p, "/")
}

// entryFinderInfo builds the 32-byte classic FInfo record (type, creator,
// Finder flags — the fields StuffIt's catalog actually carries) from a
// parsed Entry, matching the layout expandMacBinary already produces from
// its own header fields.
func entryFinderInfo(entry stuffit.Entry) [32]byte {
	var fi [32]byte
	bp.PutBE32(fi[0:4], entry.FileType)
	bp.PutBE32(fi[4:8], entry.FileCreator)
	bp.PutBE16(fi[8:10], entry.FinderFlags)
	return fi
}
