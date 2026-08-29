package archive

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
)

func isZip(data []byte) bool {
	return len(data) >= 4 && data[0] == 'P' && data[1] == 'K'
}

func expandZip(data []byte) ([]Node, error) {
	if !isZip(data) {
		return nil, ErrUnsupportedFormat
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrCorrupt
	}
	tb := newTreeBuilder()
	for _, f := range r.File {
		p := strings.TrimPrefix(strings.ReplaceAll(f.Name, "\\", "/"), "./")
		if p == "" || strings.Contains(p, "..") {
			continue
		}
		if strings.HasSuffix(p, "/") {
			tb.addDir(strings.TrimSuffix(p, "/"))
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		base := p
		if i := strings.LastIndex(p, "/"); i >= 0 {
			base = p[i+1:]
		}
		if strings.HasPrefix(base, "._") && len(base) > 2 {
			target := strings.TrimSuffix(p[:len(p)-len(base)], "/")
			if target != "" {
				target = target + "/" + base[2:]
			} else {
				target = base[2:]
			}
			if ad := parseAppleDouble(body); ad != nil {
				tb.setResource(target, ad.resource, ad.finderInfo)
			}
			continue
		}
		if strings.EqualFold(base, ".DS_Store") {
			continue
		}
		tb.setData(p, body)
	}
	roots := tb.roots()
	if len(roots) == 0 {
		return nil, ErrCorrupt
	}
	return roots, nil
}

type appleDouble struct {
	resource   []byte
	finderInfo [32]byte
}

func parseAppleDouble(b []byte) *appleDouble {
	if len(b) < 26 {
		return nil
	}
	magic := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	if magic != 0x00051607 {
		return nil
	}
	n := int(uint32(b[4])<<24 | uint32(b[5])<<16 | uint32(b[6])<<8 | uint32(b[7]))
	if n <= 0 || 26+n*12 > len(b) {
		return nil
	}
	var rsrcOff, rsrcLen int
	var fiOff, fiLen int
	for i := 0; i < n; i++ {
		off := 26 + i*12
		id := uint32(b[off])<<24 | uint32(b[off+1])<<16 | uint32(b[off+2])<<8 | uint32(b[off+3])
		start := int(uint32(b[off+4])<<24 | uint32(b[off+5])<<16 | uint32(b[off+6])<<8 | uint32(b[off+7]))
		length := int(uint32(b[off+8])<<24 | uint32(b[off+9])<<16 | uint32(b[off+10])<<8 | uint32(b[off+11]))
		switch id {
		case 2:
			rsrcOff, rsrcLen = start, length
		case 9:
			fiOff, fiLen = start, length
		}
	}
	out := &appleDouble{}
	if rsrcLen > 0 && rsrcOff+rsrcLen <= len(b) {
		out.resource = append([]byte(nil), b[rsrcOff:rsrcOff+rsrcLen]...)
	}
	if fiLen >= 32 && fiOff+32 <= len(b) {
		copy(out.finderInfo[:], b[fiOff:fiOff+32])
	}
	return out
}
