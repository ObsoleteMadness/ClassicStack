package afp

import "github.com/ObsoleteMadness/ClassicStack/core/fs"

// AFP path-type bytes (Inside Macintosh: Networking, AFP 2.x §5). The path-type
// byte prefixes every AFP pathname argument and selects the wire charset of the
// name bytes that follow it. The service threads this through to the share's
// FilenameCodec on every Decode/Encode — it never hard-wires MacRoman — so one
// volume can serve classic (MacRoman) and modern (UTF-8) clients at once.
const (
	// PathTypeShortNames is the 8.3 short-name path type (MacRoman bytes).
	PathTypeShortNames uint8 = 1
	// PathTypeLongNames is the 31-byte long-name path type (MacRoman bytes).
	PathTypeLongNames uint8 = 2
	// PathTypeUTF8Names is the UTF-8 path type (kFPUTF8Name).
	PathTypeUTF8Names uint8 = 3
)

// wireFor maps an AFP path-type byte to the FilenameCodec wire charset. Short
// and long names both arrive as MacRoman on the wire; only the length budget and
// the name-engine kind differ (handled by the volume), not the charset. UTF-8
// path types map to WireUTF8. An unknown path type falls back to MacRoman, the
// pre-OS-9 default, matching the old fixed encoding.MacRomanToUTF8 path.
func wireFor(pathType uint8) fs.WireEncoding {
	switch pathType {
	case PathTypeUTF8Names:
		return fs.WireUTF8
	case PathTypeShortNames, PathTypeLongNames:
		return fs.WireMacRoman
	default:
		return fs.WireMacRoman
	}
}
