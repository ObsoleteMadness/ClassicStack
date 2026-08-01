package smb

// netserverenum.go is the client-direction RAP NetServerEnum2 call ([MS-RAP] §2.5.5):
// the browser server-list enumeration a "net view" uses. It rides an SMB_COM_TRANSACTION
// on the IPC$ \PIPE\LANMAN pipe exactly like NetShareEnum (client.go), so the framing is
// modelled on BuildNetShareEnum; only the RAP function code, descriptor strings, and
// SERVER_INFO_1 record layout differ.
//
// NetServerEnum2 asks a master/backup browser for the list of servers it knows in a
// domain, filtered by an SV_TYPE_* bitmask (0xFFFFFFFF = every type). It is the
// authoritative "who is on this workgroup" query — ordinary hosts announce only to the
// master browser, so a broadcast solicit sees far fewer servers than this returns.
//
// Reference: [MS-RAP] §2.5.5 NetServerEnum2; SERVER_INFO_1 layout ([MS-RAP] §2.5.5.2 /
// the historical LAN Manager sv1_* struct). Descriptor strings match what smbclient's
// -L / rpcclient netserverenum and mars_nwe's browser emit.

import (
	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// RAP NetServerEnum2 constants (the SMB_COM_TRANSACTION on IPC$ \PIPE\LANMAN).
const (
	rapNetServerEnum2 uint16 = 0x0068 // NetServerEnum2 function code (104)
	// Descriptor strings for level 1, byte-for-byte from a real WfW/Win98 redirector —
	// captures/win98nbf-win31nbf.pcapng frame 49 RAP block:
	//   68 00 "WrLehDO\0" "B16BBDz\0" 01 00  00 20  ff ff ff ff
	// ParamDesc "WrLehDO": level W, receive-buffer r/L, entries-read e, available h,
	// server-type-mask D, and the domain as a NULL POINTER "O" (send no domain string —
	// enumerate the server's own primary domain). ReturnDesc "B16BBDz": SERVER_INFO_1 =
	// name B16, version-major B, version-minor B, server-type D, comment pointer z.
	rapNetServerEnum2ParamDesc  = "WrLehDO"
	rapNetServerEnum2ReturnDesc = "B16BBDz"
	rapServerInfo1Level         = 1 // detail level 1 → SERVER_INFO_1
	// rapNetServerEnum2ReplyParamLen is the reply parameter block size: Status(2) +
	// Converter(2) + EntriesReturned(2) + EntriesAvailable(2) = 8, the same shape as
	// NetShareEnum. Sending the receive-buffer length here misframes Win98's reply.
	rapNetServerEnum2ReplyParamLen = 8
)

// serverInfo1Size is the on-wire SERVER_INFO_1 record: name(16) + version-major(1) +
// version-minor(1) + server-type(4) + comment-pointer(4) = 26 bytes ([MS-RAP]).
const serverInfo1Size = 26

// ServerTypeAll requests every server type in a NetServerEnum2 server-list (level 1) call.
// It is the FULL 0xFFFFFFFF mask — the DOMAIN_ENUM bit (0x80000000) is INCLUDED, exactly as
// a real WfW/Win98 redirector sends it (captures/win98nbf-win31nbf.pcapng frame 49). The
// domain enumeration is distinguished by the detail LEVEL (0 vs 1), not by clearing this
// bit: a level-1 request with servertype 0x7FFFFFFF (DOMAIN_ENUM cleared) is what Win98
// rejected with RAP status 0x0001 (ERROR_INVALID_FUNCTION) — observed live.
const ServerTypeAll uint32 = 0xFFFFFFFF

// ServerInfo is one enumerated browser-list server: its name, the SV_TYPE_* bits it
// advertises, its OS/browser version, and the operator comment.
type ServerInfo struct {
	Name         string
	VersionMajor uint8
	VersionMinor uint8
	Type         uint32
	Comment      string
}

// BuildNetServerEnum2 builds the SMB_COM_TRANSACTION request that carries a RAP
// NetServerEnum2 (level 1) over the IPC$ \PIPE\LANMAN pipe. serverType is the SV_TYPE_*
// bitmask to filter by (ServerTypeAll for every server). The TID must already name the IPC$
// tree. The framing mirrors BuildNetShareEnum: the transaction parameter area is the RAP
// request (function + descriptors + level + receive-buffer + type-mask), no data.
//
// The domain is sent as a RAP NULL POINTER ("O" in the param descriptor) — the server
// enumerates its own primary domain, exactly as a real WfW/Win98 redirector does. There is
// therefore NO domain string on the wire; the domain argument is retained on the API for
// callers/back-compat but is intentionally not marshalled (a NUL-terminated empty domain,
// which the old "WrLehDz" descriptor implied, made Win98 reject the call with RAP status
// 0x0001 / ERROR_INVALID_FUNCTION — captures/win98nbf-win31nbf.pcapng frame 49).
func (b *Builder) BuildNetServerEnum2(serverType uint32, domain string) []byte {
	_ = domain // the "O" (null pointer) descriptor sends no domain string; see doc above.
	// RAP request parameter block.
	rap := make([]byte, 0, 48)
	rap = bp.AppendLE16(rap, rapNetServerEnum2)
	rap = append(rap, []byte(rapNetServerEnum2ParamDesc)...)
	rap = append(rap, 0)
	rap = append(rap, []byte(rapNetServerEnum2ReturnDesc)...)
	rap = append(rap, 0)
	rap = bp.AppendLE16(rap, rapServerInfo1Level)
	rap = bp.AppendLE16(rap, rapReceiveBufferLen)
	rap = bp.AppendLE32(rap, serverType) // the "D" server-type mask
	// No domain bytes: the "O" descriptor passes the domain as a null pointer.

	name := lanmanPipe + "\x00" // the transaction Name (the pipe), OEM/ASCII

	// SMB_COM_TRANSACTION request, WCT=14 ([MS-CIFS] §2.2.4.33.1). Setup words = 0.
	const wct = 14
	words := make([]byte, wct*2)
	bp.PutLE16(words[0:2], uint16(len(rap)))               // TotalParameterCount
	bp.PutLE16(words[2:4], 0)                              // TotalDataCount
	bp.PutLE16(words[4:6], rapNetServerEnum2ReplyParamLen) // MaxParameterCount (reply param block)
	bp.PutLE16(words[6:8], rapReceiveBufferLen)            // MaxDataCount (max reply data — the records)
	words[8] = 0                                           // MaxSetupCount
	bp.PutLE16(words[18:20], uint16(len(rap)))             // ParameterCount
	bp.PutLE16(words[22:24], 0)                            // DataCount
	words[26] = 0                                          // SetupCount

	// Byte area: Name\0, then (2-byte aligned) the RAP parameters.
	area := make([]byte, 0, len(name)+1+len(rap))
	area = append(area, []byte(name)...)
	base := HeaderLen + 1 + wct*2 + 2 // header + WCT + words + ByteCount
	paramOff := base + len(area)
	if paramOff%2 != 0 {
		area = append(area, 0)
		paramOff++
	}
	area = append(area, rap...)

	bp.PutLE16(words[20:22], uint16(paramOff))          // ParameterOffset
	bp.PutLE16(words[24:26], uint16(paramOff+len(rap))) // DataOffset (no data; points past params)

	return b.frame(CommandTransaction, words, area)
}

// ParseNetServerEnum2 parses the SMB_COM_TRANSACTION response to a RAP NetServerEnum2.
// The transaction parameter block is Status(2)+Converter(2)+EntriesReturned(2)+
// EntriesAvailable(2); the data block is EntriesReturned SERVER_INFO_1 records (26 bytes
// each) followed by a comment heap. Each record's comment pointer low word is the offset
// of its NUL-terminated comment within the data block, biased by Converter (subtracted).
// A non-zero RAP Status is returned as a *RAPError; the special status 234 (ERROR_MORE_DATA)
// is tolerated — the returned entries are still valid, the reply was just truncated.
func ParseNetServerEnum2(resp []byte) ([]ServerInfo, error) {
	if _, _, _, err := respBody(CommandTransaction, resp); err != nil {
		return nil, err
	}
	params, data, err := transactionResponse(resp)
	if err != nil {
		return nil, err
	}
	if len(params) < 8 {
		return nil, ErrShortResponse
	}
	status := bp.LE16(params[0:2])
	// ERROR_MORE_DATA (234) means the buffer held only some of the servers; the records we
	// did get are valid, so parse them rather than failing the whole enumeration.
	if status != 0 && status != rapStatusMoreData {
		return nil, &RAPError{Status: status}
	}
	converter := bp.LE16(params[2:4])
	entries := int(bp.LE16(params[4:6]))

	out := make([]ServerInfo, 0, entries)
	for i := range entries {
		base := i * serverInfo1Size
		if base+serverInfo1Size > len(data) {
			break
		}
		rec := data[base : base+serverInfo1Size]
		si := ServerInfo{
			Name:         oemString(rec[0:16]), // name, NUL-padded within 16
			VersionMajor: rec[16],
			VersionMinor: rec[17],
			Type:         bp.LE32(rec[18:22]),
		}
		// The comment pointer (low word) is a data-relative offset once the Converter bias
		// is removed.
		ptr := bp.LE16(rec[22:24])
		if ptr >= converter {
			off := int(ptr - converter)
			if off >= 0 && off < len(data) {
				si.Comment = oemStringZ(data[off:])
			}
		}
		out = append(out, si)
	}
	return out, nil
}

// rapStatusMoreData is the RAP/Win32 ERROR_MORE_DATA status (234): the reply buffer held
// only part of the list. The entries returned are still valid.
const rapStatusMoreData uint16 = 234
