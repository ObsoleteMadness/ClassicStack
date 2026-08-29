package ncp

// hexfmt.go hand-rolls the small hex-formatting helpers the NCP service needs for
// its diagnostic logs and endpoint strings. Core packages may not import fmt: fmt
// transitively pulls in reflect, which the §1 no-reflection rule (TinyGo +
// allocation discipline, enforced by core/internal/archtest) forbids in the core
// ring. These byte-for-byte replace the fmt.Sprintf calls they supersede.

// hexLower is the lowercase hex alphabet used for the net.node endpoint form.
const hexLower = "0123456789abcdef"

// hexUpper is the uppercase hex alphabet used for the 0x-prefixed function/completion
// codes (matching the "0x%02X"/"0x%04X" spelling the logs used before).
const hexUpper = "0123456789ABCDEF"

// hexBytes renders a byte slice as lowercase hex with no separators (the "%x" verb
// on a []byte / fixed array).
func hexBytes(b []byte) string {
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, hexLower[c>>4], hexLower[c&0x0F])
	}
	return string(out)
}

// hex8 renders a byte as "0xNN" with two uppercase hex digits (the "0x%02X" verb).
func hex8(v byte) string {
	return "0x" + string([]byte{hexUpper[v>>4], hexUpper[v&0x0F]})
}
