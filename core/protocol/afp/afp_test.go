package afp

import (
	"bytes"
	"testing"
	"time"
)

// TestLoginMarshal checks the FPLogin block shape for both UAMs.
func TestLoginMarshal(t *testing.T) {
	guest := LoginRequest{AFPVersion: AFPVersion21, UAM: UAMNoUserAuthent}.Marshal()
	// cmd(1) + pstring("AFPVersion 2.1") + pstring("No User Authent")
	if guest[0] != CmdLogin {
		t.Fatalf("cmd = %d, want %d", guest[0], CmdLogin)
	}
	ver, off, ok := PString(guest, 1)
	if !ok || string(ver) != AFPVersion21 {
		t.Fatalf("version = %q ok=%v", ver, ok)
	}
	uam, _, ok := PString(guest, off)
	if !ok || string(uam) != UAMNoUserAuthent {
		t.Fatalf("uam = %q ok=%v", uam, ok)
	}

	clear := LoginRequest{AFPVersion: AFPVersion21, UAM: UAMCleartext, User: "pete", Pass: "secret"}.Marshal()
	_, off2, _ := PString(clear, 1)
	_, off3, _ := PString(clear, off2)
	user, off4, ok := PString(clear, off3)
	if !ok || string(user) != "pete" {
		t.Fatalf("user = %q", user)
	}
	if len(clear)-off4 != 8 {
		t.Fatalf("password field = %d bytes, want 8", len(clear)-off4)
	}
	if !bytes.HasPrefix(clear[off4:], []byte("secret")) {
		t.Errorf("password field = %q, want secret-prefixed", clear[off4:])
	}
}

// TestLoginMarshalServerUAMSpelling is the regression for the credential trailer being
// keyed on the guest UAM rather than an exact match against the capital-P UAMCleartext
// constant: a real server advertises the cleartext UAM under its own spelling
// ("Cleartxt passwrd", lower-case p) and the client echoes that exact string, so the
// username + 8-byte password MUST still be appended. Keying on == UAMCleartext dropped
// them for the lower-case spelling and the login carried no credentials (System 7.5
// silently discarded it).
func TestLoginMarshalServerUAMSpelling(t *testing.T) {
	const serverUAM = "Cleartxt passwrd" // the lower-case spelling a real Mac advertises
	blk := LoginRequest{AFPVersion: AFPVersion21, UAM: serverUAM, User: "pete", Pass: ""}.Marshal()

	_, o2, _ := PString(blk, 1)      // version
	uam, o3, _ := PString(blk, o2)   // uam
	user, o4, ok := PString(blk, o3) // username
	if string(uam) != serverUAM {
		t.Fatalf("uam = %q, want %q", uam, serverUAM)
	}
	if !ok || string(user) != "pete" {
		t.Fatalf("username missing: user=%q ok=%v (credential trailer was dropped)", user, ok)
	}
	if len(blk)-o4 != 8 {
		t.Fatalf("password field = %d bytes, want 8 (empty password still needs the field)", len(blk)-o4)
	}
}

// TestParseServerInfo parses an FPGetSrvrInfo block shaped like a real System 7.5 Mac's
// and checks version/UAM extraction plus PickVersion choosing the newest advertised. The
// block is assembled append-style with the offset header patched afterwards, mirroring
// the wire layout ParseServerInfo reads.
func TestParseServerInfo(t *testing.T) {
	name := "vmac1"
	machine := "Macintosh"
	versions := []string{"AFPVersion 1.1", "AFPVersion 2.0", "AFPVersion 2.1"}
	uams := []string{"Cleartxt passwrd", "Randnum exchange"}

	const headerLen = 10
	b := make([]byte, headerLen) // 4 offsets + Flags, patched below
	b = PutPString(b, []byte(name))
	if len(b)%2 != 0 {
		b = append(b, 0) // pad ServerName to an even boundary
	}
	machineOff := len(b)
	b = PutPString(b, []byte(machine))
	versOff := len(b)
	b = append(b, byte(len(versions)))
	for _, v := range versions {
		b = PutPString(b, []byte(v))
	}
	uamOff := len(b)
	b = append(b, byte(len(uams)))
	for _, u := range uams {
		b = PutPString(b, []byte(u))
	}
	// Patch the offset header.
	b[0], b[1] = byte(machineOff>>8), byte(machineOff)
	b[2], b[3] = byte(versOff>>8), byte(versOff)
	b[4], b[5] = byte(uamOff>>8), byte(uamOff)
	b[8], b[9] = 0x00, 0x01 // Flags

	si, ok := ParseServerInfo(b)
	if !ok {
		t.Fatal("ParseServerInfo returned ok=false")
	}
	if si.ServerName != name || si.MachineType != machine {
		t.Errorf("name/machine = %q/%q, want %q/%q", si.ServerName, si.MachineType, name, machine)
	}
	if len(si.AFPVersions) != 3 || si.AFPVersions[2] != "AFPVersion 2.1" {
		t.Errorf("versions = %v", si.AFPVersions)
	}
	if !si.HasUAM("Cleartxt passwrd") {
		t.Errorf("HasUAM(Cleartxt passwrd) = false; uams=%v", si.UAMs)
	}
	if got := si.PickVersion(); got != "AFPVersion 2.1" {
		t.Errorf("PickVersion = %q, want AFPVersion 2.1", got)
	}
}

// TestGetSrvrParmsRoundTrip marshals a reply the way the server would and parses it.
func TestGetSrvrParmsRoundTrip(t *testing.T) {
	// Build a server-shaped reply: time + 2 volumes.
	body := []byte{0, 0, 0, 0, 2}
	body[0], body[1], body[2], body[3] = 0x11, 0x22, 0x33, 0x44
	body = append(body, 0)
	body = PutPString(body, []byte("Macintosh HD"))
	body = append(body, 0)
	body = PutPString(body, []byte("Backup"))

	r, ok := ParseGetSrvrParmsReply(body)
	if !ok {
		t.Fatal("parse failed")
	}
	if r.ServerTime != 0x11223344 {
		t.Errorf("ServerTime = %#x", r.ServerTime)
	}
	if len(r.Volumes) != 2 || r.Volumes[0].Name != "Macintosh HD" || r.Volumes[1].Name != "Backup" {
		t.Errorf("Volumes = %+v", r.Volumes)
	}
}

// TestVolParamsRoundTrip builds a volume-parameter block the way packVolParams does and
// asserts ParseVolParams recovers the fields, including the offset-addressed Name.
func TestVolParamsRoundTrip(t *testing.T) {
	const bitmap = VolBitmapAttributes | VolBitmapSignature | VolBitmapID |
		VolBitmapBytesFree | VolBitmapBytesTotal | VolBitmapName

	// Fixed area sizes: attr(2)+sig(2)+id(2)+free(4)+total(4)+nameptr(2) = 16.
	fixedSize := 16
	var fixed, variable []byte
	fixed = appendBE16(fixed, 0x0000) // attributes
	fixed = appendBE16(fixed, VolSignatureFixedDirID)
	fixed = appendBE16(fixed, 7)                 // volID
	fixed = appendBE32(fixed, 1000)              // free
	fixed = appendBE32(fixed, 2000)              // total
	fixed = appendBE16(fixed, uint16(fixedSize)) // name offset (points past fixed)
	variable = PutPString(variable, []byte("MyVol"))

	body := appendBE16(nil, bitmap)
	body = append(body, fixed...)
	body = append(body, variable...)

	v, ok := ParseVolParams(body)
	if !ok {
		t.Fatal("parse failed")
	}
	if v.Signature != VolSignatureFixedDirID || v.VolID != 7 {
		t.Errorf("sig/id = %d/%d", v.Signature, v.VolID)
	}
	if v.BytesFree != 1000 || v.BytesTotal != 2000 {
		t.Errorf("free/total = %d/%d", v.BytesFree, v.BytesTotal)
	}
	if v.Name != "MyVol" {
		t.Errorf("name = %q", v.Name)
	}
}

// TestFileParamsRoundTrip packs a file parameter block matching the server layout
// (fixed fields in bit order, names in a trailing offset-addressed area) and asserts
// ParseFileDirParams recovers every field.
func TestFileParamsRoundTrip(t *testing.T) {
	const bitmap = FDBitmapModDate | FDBitmapFinderInfo | FDBitmapLongName |
		FileBitmapFileNum | FileBitmapDataForkLen | FileBitmapRsrcForkLen

	mod := time.Date(2001, 6, 15, 12, 0, 0, 0, time.UTC)
	var fi [32]byte
	copy(fi[0:4], "TEXT")
	copy(fi[4:8], "ttxt")

	// Fixed area: modDate(4)+finder(32)+nameptr(2)+fileNum(4)+dataLen(4)+rsrcLen(4)=50.
	fixedSize := 50
	var fixed, names []byte
	fixed = appendBE32(fixed, MacTime(mod))
	fixed = append(fixed, fi[:]...)
	fixed = appendBE16(fixed, uint16(fixedSize)) // long-name offset
	names = PutPString(names, []byte("readme.txt"))
	fixed = appendBE32(fixed, 42)   // fileNum
	fixed = appendBE32(fixed, 1024) // dataForkLen
	fixed = appendBE32(fixed, 256)  // rsrcForkLen

	block := append(append([]byte(nil), fixed...), names...)

	p := ParseFileDirParams(block, bitmap, false)
	if !p.ModDate.Equal(mod) {
		t.Errorf("ModDate = %v, want %v", p.ModDate, mod)
	}
	if !bytes.Equal(p.FinderInfo[0:8], fi[0:8]) {
		t.Errorf("FinderInfo = %q", p.FinderInfo[0:8])
	}
	if string(p.LongName) != "readme.txt" {
		t.Errorf("LongName = %q", p.LongName)
	}
	if p.FileNum != 42 || p.DataForkLen != 1024 || p.RsrcForkLen != 256 {
		t.Errorf("fileNum/data/rsrc = %d/%d/%d", p.FileNum, p.DataForkLen, p.RsrcForkLen)
	}
}

// TestMacTimeRoundTrip checks the AFP timestamp conversion round-trips.
func TestMacTimeRoundTrip(t *testing.T) {
	tm := time.Date(2005, 3, 1, 8, 30, 0, 0, time.UTC)
	if got := FromMacTime(MacTime(tm)); !got.Equal(tm) {
		t.Errorf("round trip = %v, want %v", got, tm)
	}
	if !FromMacTime(NoBackupDate).IsZero() {
		t.Errorf("NoBackupDate should map to zero time")
	}
}

// TestWriteHeaderReqCount asserts the FPWrite header carries the data length as
// reqCount (the server reads it from bytes 8:12).
func TestWriteHeaderReqCount(t *testing.T) {
	w := WriteRequest{ForkRefNum: 3, Offset: 0, Data: []byte("hello")}
	h := w.Header()
	if len(h) != 12 {
		t.Fatalf("header = %d bytes, want 12", len(h))
	}
	if got := beU32(h[8:12]); got != 5 {
		t.Errorf("reqCount = %d, want 5", got)
	}
}

// --- small BE helpers so the test file is self-contained (production code uses
// core/binaryprimitives). ---

func appendBE16(b []byte, v uint16) []byte { return append(b, byte(v>>8), byte(v)) }
func appendBE32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
func beU32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
