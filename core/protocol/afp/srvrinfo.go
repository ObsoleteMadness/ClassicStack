package afp

import bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

// srvrinfo.go holds the CLIENT-direction parser for the FPGetSrvrInfo reply block —
// the server-status block ASPGetStatus returns, listing the machine type, the AFP
// version strings the server accepts at FPLogin, and the User Authentication Methods
// (UAMs) it offers. A real AppleShare client reads this to pick the newest version and
// a supported UAM it shares with the server, rather than guessing — which is exactly
// what a System 7.x server requires: it silently ignores an FPLogin naming a version
// string it never advertised (observed: a System 7.5 Mac offers "AFPVersion 2.1", not
// "AFP2.2", and "Cleartxt passwrd" with a lower-case p — see spec/errata.md).
//
// Layout (Inside Macintosh: Networking, "GetSrvrInfo reply"), all offsets from the
// start of the block, big-endian:
//
//	uint16 offset to MachineType
//	uint16 offset to AFP-version count
//	uint16 offset to UAM count
//	uint16 offset to icon/mask (0 = none)
//	uint16 Flags
//	pstring ServerName            (immediately after the header)
//	(pad to even)
//	pstring MachineType
//	uint8 versionCount; pstring × versionCount
//	uint8 uamCount;     pstring × uamCount
//
// This mirrors core/service/afp/handlers.go serverInfoBlock (CLAUDE.md rule #10:
// the block is a DTO, parsed here rather than byte-picked at the call site).

// ServerInfo is the parsed FPGetSrvrInfo block a client uses to negotiate the login.
type ServerInfo struct {
	Flags       uint16
	ServerName  string
	MachineType string
	// AFPVersions are the version strings the server accepts at FPLogin, in the order
	// advertised (oldest→newest, matching AppleShare). PickVersion selects from these.
	AFPVersions []string
	// UAMs are the User Authentication Method names offered (e.g. "No User Authent",
	// "Cleartxt passwrd", "Randnum exchange"). HasUAM reports membership.
	UAMs []string
}

// ParseServerInfo decodes an FPGetSrvrInfo reply block. It reads the version and UAM
// lists from their declared offsets (a truncated or inconsistent block yields the
// fields parsed so far; ok is false only when the fixed header is missing).
func ParseServerInfo(b []byte) (ServerInfo, bool) {
	const headerLen = 10 // 4 offsets + Flags
	if len(b) < headerLen {
		return ServerInfo{}, false
	}
	var info ServerInfo
	machineOff := int(bp.BE16(b[0:2]))
	versOff := int(bp.BE16(b[2:4]))
	uamOff := int(bp.BE16(b[4:6]))
	info.Flags = bp.BE16(b[8:10])

	if name, _, ok := PString(b, headerLen); ok {
		info.ServerName = string(name)
	}
	if machineOff > 0 && machineOff < len(b) {
		if mt, _, ok := PString(b, machineOff); ok {
			info.MachineType = string(mt)
		}
	}
	info.AFPVersions = parsePStringList(b, versOff)
	info.UAMs = parsePStringList(b, uamOff)
	return info, true
}

// parsePStringList reads a count byte at off then that many Pascal strings. A bad
// offset or a truncated list yields the entries read so far (nil for a zero offset).
func parsePStringList(b []byte, off int) []string {
	if off <= 0 || off >= len(b) {
		return nil
	}
	count := int(b[off])
	off++
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		s, next, ok := PString(b, off)
		if !ok {
			break
		}
		out = append(out, string(s))
		off = next
	}
	return out
}

// SupportsSrvrMsg reports whether Flags advertises server-message support
// (SrvrInfoSupportsSrvrMsg). Without it a classic client neither fetches the
// login greeting nor honours message attentions.
func (si ServerInfo) SupportsSrvrMsg() bool {
	return si.Flags&SrvrInfoSupportsSrvrMsg != 0
}

// HasUAM reports whether the server offers the named UAM (case-sensitive — classic AFP
// UAM names are matched exactly, and their case varies by server, e.g. "Cleartxt
// passwrd").
func (si ServerInfo) HasUAM(name string) bool {
	for _, u := range si.UAMs {
		if u == name {
			return true
		}
	}
	return false
}

// afpVersionRank orders the AFP version strings this client understands, newest
// highest. An unrecognised string ranks 0 so a known version always wins.
var afpVersionRank = map[string]int{
	"AFPVersion 1.1": 1,
	"AFPVersion 2.0": 2,
	"AFPVersion 2.1": 3,
	"AFP2.2":         4,
	"AFPVersion 2.2": 4,
	"AFPX03":         5,
	"AFP3.0":         5,
}

// PickVersion returns the newest AFP version the server advertised that this client
// understands, using the server's exact advertised string. It returns "" when the
// server advertised no version this client ranks (the caller then falls back to a
// default). Preferring the server's own string is essential — a System 7.x server
// ignores an FPLogin whose version string it did not advertise.
func (si ServerInfo) PickVersion() string {
	best, bestRank := "", 0
	for _, v := range si.AFPVersions {
		if r := afpVersionRank[v]; r > bestRank {
			best, bestRank = v, r
		}
	}
	return best
}
