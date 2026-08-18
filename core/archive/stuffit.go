package archive

import (
	"strings"
)

func isStuffIt(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	sig := string(data[0:4])
	return sig == "SIT!" || sig == "SIT5" || sig == "SITD"
}

// expandStuffIt expands StuffIt 1.x archives. StuffIt 5 and packed formats return
// ErrUnsupportedFormat until a full port lands (see ClassicStack-web/src/fs/stuffit.ts).
func expandStuffIt(name string, data []byte, finderInfo [32]byte) ([]Node, error) {
	_ = name
	if len(finderInfo) >= 4 {
		t := strings.TrimRight(string(finderInfo[0:4]), "\x00")
		if t == "SIT!" || t == "SIT5" || t == "SITD" {
			if !isStuffIt(data) {
				return nil, ErrUnsupportedFormat
			}
		}
	}
	if !isStuffIt(data) {
		return nil, ErrUnsupportedFormat
	}
	sig := string(data[0:4])
	if sig != "SIT!" {
		return nil, ErrUnsupportedFormat
	}
	return expandStuffItClassic(data)
}

func expandStuffItClassic(data []byte) ([]Node, error) {
	_ = data
	return nil, ErrUnsupportedFormat
}
