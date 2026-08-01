package smb

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
)

// SV_TYPE_* bits ([MS-BRWS]) used to build test records; the parser does not interpret
// them, so these are just fixture values to prove round-trip fidelity of the type word.
const (
	svTypeWorkstation  uint32 = 0x00000001
	svTypeServer       uint32 = 0x00000002
	svTypeMasterBrowse uint32 = 0x00040000
)

// TestBuildNetServerEnum2Request checks the RAP NetServerEnum2 request matches the real
// WfW/Win98 wire shape (captures/win98nbf-win31nbf.pcapng frame 49): WCT=14, the
// \PIPE\LANMAN transaction name, the "WrLehDO"/"B16BBDtz" descriptors + level 1, the
// server-type mask, MaxParameterCount = 8, and — the load-bearing part — NO trailing domain
// string (the "O" descriptor passes the domain as a null pointer). Sending a NUL-terminated
// domain with the old "WrLehDz" made Win98 reject the call with RAP status 0x0001.
func TestBuildNetServerEnum2Request(t *testing.T) {
	b := &Builder{PID: 0xFEFF, TID: 0x1234, UID: 5}
	// Pass a non-empty domain to prove it is NOT marshalled (null-pointer semantics).
	req := b.BuildNetServerEnum2(ServerTypeAll, "WORKGROUP")

	h, err := DecodeHeader(req)
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if h.Command != CommandTransaction {
		t.Fatalf("command = 0x%02x, want TRANSACTION 0x%02x", h.Command, CommandTransaction)
	}
	wct := int(req[HeaderLen])
	if wct != 14 {
		t.Fatalf("WCT = %d, want 14", wct)
	}
	w := req[HeaderLen+1 : HeaderLen+1+2*wct]
	if got := bp.LE16(w[4:6]); got != rapNetServerEnum2ReplyParamLen {
		t.Errorf("MaxParameterCount = %d, want %d", got, rapNetServerEnum2ReplyParamLen)
	}
	for _, want := range []string{`\PIPE\LANMAN`, "WrLehDO", "B16BBDz"} {
		if indexOf(req, want) < 0 {
			t.Errorf("request missing %q", want)
		}
	}
	// The domain must NOT appear on the wire — it is a null pointer ("O").
	if indexOf(req, "WORKGROUP") >= 0 {
		t.Error("request carries a domain string, but the \"O\" descriptor must send none")
	}
	// The old ParamDesc must be gone (it triggers Win98 ERROR_INVALID_FUNCTION).
	if indexOf(req, "WrLehDz") >= 0 {
		t.Error("request still carries the old \"WrLehDz\" descriptor")
	}
	// The server-type mask must be the FULL 0xFFFFFFFF (DOMAIN_ENUM bit included), the WfW
	// form — a cleared bit (0x7FFFFFFF) is what Win98 rejected. It is the last 4 bytes.
	if got := bp.LE32(req[len(req)-4:]); got != 0xFFFFFFFF {
		t.Errorf("server-type mask = %#08x, want 0xFFFFFFFF (full WfW mask)", got)
	}
}

// TestParseNetServerEnum2Response round-trips a browser-shaped NetServerEnum2 reply
// (SERVER_INFO_1 records + comment heap) through the client parser and checks the names,
// server-type bits, versions, and comments resolve.
func TestParseNetServerEnum2Response(t *testing.T) {
	servers := []struct {
		name     string
		verMajor uint8
		verMinor uint8
		typ      uint32
		comment  string
	}{
		{"NW-MASTER", 4, 9, svTypeServer | svTypeMasterBrowse, "The master"},
		{"WIN98BOX", 4, 10, svTypeWorkstation, "A workstation"},
		{"SILENT", 5, 0, svTypeServer, ""},
	}

	remarkBase := len(servers) * serverInfo1Size
	remarkOff := remarkBase
	remarks := make([]byte, 0)
	offs := make([]int, len(servers))
	for i, s := range servers {
		offs[i] = remarkOff
		remarks = append(remarks, []byte(s.comment)...)
		remarks = append(remarks, 0)
		remarkOff += len(s.comment) + 1
	}
	data := make([]byte, remarkBase+len(remarks))
	for i, s := range servers {
		base := i * serverInfo1Size
		copy(data[base:base+16], s.name)
		data[base+16] = s.verMajor
		data[base+17] = s.verMinor
		bp.PutLE32(data[base+18:base+22], s.typ)
		bp.PutLE16(data[base+22:base+24], uint16(offs[i]))
	}
	copy(data[remarkBase:], remarks)

	params := make([]byte, 8) // Status(2)+Converter(2)+EntriesReturned(2)+EntriesAvailable(2)
	bp.PutLE16(params[4:6], uint16(len(servers)))
	bp.PutLE16(params[6:8], uint16(len(servers)))

	resp := buildTestTransactionResponse(params, data)

	got, err := ParseNetServerEnum2(resp)
	if err != nil {
		t.Fatalf("ParseNetServerEnum2: %v", err)
	}
	if len(got) != len(servers) {
		t.Fatalf("parsed %d servers, want %d", len(got), len(servers))
	}
	for i, s := range servers {
		if got[i].Name != s.name || got[i].Type != s.typ || got[i].Comment != s.comment ||
			got[i].VersionMajor != s.verMajor || got[i].VersionMinor != s.verMinor {
			t.Errorf("server %d = %+v, want {%q %d %d %#x %q}", i, got[i],
				s.name, s.verMajor, s.verMinor, s.typ, s.comment)
		}
	}
}

// TestParseNetServerEnum2MoreData confirms an ERROR_MORE_DATA (234) status still yields the
// records that fit — a big segment truncates the reply, but the partial list is valid.
func TestParseNetServerEnum2MoreData(t *testing.T) {
	data := make([]byte, serverInfo1Size)
	copy(data[0:16], "ONLYONE")
	bp.PutLE32(data[18:22], svTypeServer)
	bp.PutLE16(data[22:24], 0) // no comment (ptr < converter)

	params := make([]byte, 8)
	bp.PutLE16(params[0:2], rapStatusMoreData) // Status = ERROR_MORE_DATA
	bp.PutLE16(params[4:6], 1)                 // EntriesReturned
	bp.PutLE16(params[6:8], 50)                // EntriesAvailable (more than fit)

	got, err := ParseNetServerEnum2(buildTestTransactionResponse(params, data))
	if err != nil {
		t.Fatalf("ParseNetServerEnum2 with MORE_DATA: %v", err)
	}
	if len(got) != 1 || got[0].Name != "ONLYONE" {
		t.Fatalf("parsed %+v, want the single ONLYONE record", got)
	}
}
