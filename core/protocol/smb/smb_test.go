package smb

import (
	"bytes"
	"strings"
	"testing"
)

// goldenHeaderFrame14 is the 32-byte SMB1 header from captures/ipx.pcap frame
// #14 (an SMB_COM_TRANSACTION / mailslot browse over NetBIOS-over-IPX). All
// fields beyond the command are zero on this request.
//
// This is the M2 capture-replay vector: DecodeHeader then Encode must be
// byte-identical to the wire.
var goldenHeaderFrame14 = []byte{
	0xff, 0x53, 0x4d, 0x42, // protocol identifier "\xffSMB"
	0x25,                   // [4]  command = SMB_COM_TRANSACTION
	0x00, 0x00, 0x00, 0x00, // [5]  status
	0x00,       // [9]  flags
	0x00, 0x00, // [10] flags2
	0x00, 0x00, // [12] PID high
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // [14] security features (8)
	0x00, 0x00, // [22] reserved
	0x00, 0x00, // [24] TID
	0x00, 0x00, // [26] PID low
	0x00, 0x00, // [28] UID
	0x00, 0x00, // [30] MID
}

func TestCaptureReplay_HeaderFrame14(t *testing.T) {
	t.Parallel()
	h, err := DecodeHeader(goldenHeaderFrame14)
	if err != nil {
		t.Fatalf("DecodeHeader: %v", err)
	}
	if h.Command != CommandTransaction {
		t.Errorf("Command = %#x, want Transaction", h.Command)
	}
	if h.IsResponse() {
		t.Error("IsResponse = true, want false (this is a request)")
	}

	got := h.Encode(nil)
	if !bytes.Equal(got, goldenHeaderFrame14) {
		t.Fatalf("re-encode not byte-identical:\n got % x\nwant % x", got, goldenHeaderFrame14)
	}
}

func TestHeaderRoundTrip(t *testing.T) {
	t.Parallel()
	h := Header{
		Command:  CommandNegotiate,
		Status:   0x12345678,
		Flags:    FlagReply,
		Flags2:   Flags2KnowsLongNames | Flags2NTStatus,
		PIDHigh:  0xABCD,
		Security: [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		TID:      0x1111,
		PIDLow:   0x2222,
		UID:      0x3333,
		MID:      0x4444,
	}
	got, err := DecodeHeader(h.Encode(nil))
	if err != nil {
		t.Fatalf("DecodeHeader: %v", err)
	}
	if got != h {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, h)
	}
	if !got.IsResponse() {
		t.Error("IsResponse = false, want true (FlagReply set)")
	}
}

func TestSequenceNumber(t *testing.T) {
	t.Parallel()
	// SequenceNumber sits at header offset 20 = Security[6:8], little-endian.
	h := Header{Security: [8]byte{0, 0, 0, 0, 0, 0, 0x34, 0x12}}
	if got := h.SequenceNumber(); got != 0x1234 {
		t.Fatalf("SequenceNumber = %#x, want 0x1234", got)
	}
}

func TestDecodeHeaderErrors(t *testing.T) {
	t.Parallel()
	if _, err := DecodeHeader(make([]byte, HeaderLen-1)); err != ErrShort {
		t.Errorf("short: err = %v, want ErrShort", err)
	}
	bad := make([]byte, HeaderLen)
	bad[0] = 0xEE // wrong magic
	if _, err := DecodeHeader(bad); err != ErrBadProtocol {
		t.Errorf("bad protocol: err = %v, want ErrBadProtocol", err)
	}
}

func TestEncodePreservesPrefix(t *testing.T) {
	t.Parallel()
	prefix := []byte{0xAA, 0xBB}
	got := Header{Command: CommandEcho}.Encode(prefix)
	if !bytes.HasPrefix(got, prefix) {
		t.Fatalf("Encode dropped prefix: % x", got)
	}
	if len(got) != len(prefix)+HeaderLen {
		t.Fatalf("len = %d, want %d", len(got), len(prefix)+HeaderLen)
	}
}

func TestCapabilityNames(t *testing.T) {
	if CapabilityNames(0) != nil {
		t.Fatal("zero caps should be empty")
	}
	got := CapabilityNames(CapNTSMBs | CapNTStatus | CapNTFind | CapLargeFiles)
	want := []string{"Large files", "NT SMBs", "NT status", "NT Find"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// TestCaptureReplay_OS2DialectNegotiation replays the dialect list a real OS/2 LAN
// Requester offers (golden capture spec/captures/nbf-os2-win98.pcap frame 100) and
// pins that we select what the OS/2 SERVER selected from the identical list in the
// same capture — index 4, LANMAN2.1, LanMan response family (frame 125).
//
// Two things this guards. First, XENIX CORE: it appears SECOND in every OS/2 list and
// had no rank, so it scored 0 and could never be selected; a client offering only the
// core dialects would have been told 0xFFFF ("nothing in common"). Second, the
// selection itself — Win98 answers this same list with index 2 (LANMAN1.0, frame 102),
// evidently not recognising the OS/2 spellings of LM1.2X002 / LANMAN2.1. We do
// recognise them, so we match the OS/2 server rather than Win98's narrower table.
func TestCaptureReplay_OS2DialectNegotiation(t *testing.T) {
	// Frame 100, in wire order.
	os2 := []string{
		DialectPCNetwork1,
		DialectXenixCore,
		DialectLANMAN10,
		DialectLM12X002,
		DialectLANMAN21,
	}
	idx, name, family := SelectDialect(os2)
	if idx != 4 || name != DialectLANMAN21 {
		t.Errorf("SelectDialect = %d/%q, want 4/%q (the OS/2 server's choice, frame 125)",
			idx, name, DialectLANMAN21)
	}
	if family != DialectFamilyLanMan {
		t.Errorf("family = %v, want DialectFamilyLanMan (WCT=13 response)", family)
	}

	// XENIX CORE must be selectable on its own, in the core family.
	idx, name, family = SelectDialect([]string{DialectXenixCore})
	if idx != 0 || name != DialectXenixCore {
		t.Errorf("XENIX-CORE-only = %d/%q, want 0/%q", idx, name, DialectXenixCore)
	}
	if family != DialectFamilyCore {
		t.Errorf("XENIX CORE family = %v, want DialectFamilyCore (WCT=1 response)", family)
	}
}

// goldenNegotiateTrailer is the 32-byte name trailer a real NWLink redirector appends
// to its direct-hosted-IPX NEGOTIATE — golden capture spec/captures/nwlink-win98.pcap
// frame 16, the bytes after the 119-byte dialect area. Source WIN98-IPX-1<00>
// (workstation) then destination WIN98-IPX-2<20> (file server).
var goldenNegotiateTrailer = []byte{
	'W', 'I', 'N', '9', '8', '-', 'I', 'P', 'X', '-', '1', ' ', ' ', ' ', ' ', 0x00,
	'W', 'I', 'N', '9', '8', '-', 'I', 'P', 'X', '-', '2', ' ', ' ', ' ', ' ', 0x20,
}

func TestCaptureReplay_DirectIPXNegotiateNameTrailer(t *testing.T) {
	t.Parallel()
	if len(goldenNegotiateTrailer) != NameTrailerLen {
		t.Fatalf("golden trailer is %d bytes, want NameTrailerLen (%d)",
			len(goldenNegotiateTrailer), NameTrailerLen)
	}
	var source, dest [16]byte
	copy(source[:], goldenNegotiateTrailer[:16])
	copy(dest[:], goldenNegotiateTrailer[16:])

	// A NEGOTIATE with a 4-byte dialect area, so the split must use WCT/BCC rather
	// than the datagram length to find where the message ends.
	msg := append(goldenHeaderFrame14[:HeaderLen:HeaderLen], 0x00, 0x04, 0x00)
	msg = append(msg, 0x02, 'A', 'B', 0x00)
	msg[4] = CommandNegotiate

	datagram := AppendNameTrailer(append([]byte(nil), msg...), source, dest)
	if !bytes.Equal(datagram[len(msg):], goldenNegotiateTrailer) {
		t.Fatalf("appended trailer not byte-identical to golden:\n got % x\nwant % x",
			datagram[len(msg):], goldenNegotiateTrailer)
	}

	gotMsg, gotSrc, gotDst, ok := SplitNameTrailer(datagram)
	if !ok {
		t.Fatal("SplitNameTrailer reported no trailer on a datagram carrying one")
	}
	if !bytes.Equal(gotMsg, msg) {
		t.Errorf("split message = % x, want % x", gotMsg, msg)
	}
	if gotSrc != source || gotDst != dest {
		t.Errorf("split names = %q/%q, want %q/%q", gotSrc, gotDst, source, dest)
	}
}

func TestSplitNameTrailerAbsent(t *testing.T) {
	t.Parallel()
	// Golden frames 18/20/22/24 carry NO trailer: the message must come back whole.
	msg := append(goldenHeaderFrame14[:HeaderLen:HeaderLen], 0x00, 0x02, 0x00, 0xAA, 0xBB)
	got, src, dst, ok := SplitNameTrailer(msg)
	if ok {
		t.Errorf("SplitNameTrailer = true on a trailer-less message (names %q/%q)", src, dst)
	}
	if !bytes.Equal(got, msg) {
		t.Errorf("message = % x, want it returned unchanged (% x)", got, msg)
	}
}

func TestDOSErrStatusNaming(t *testing.T) {
	t.Parallel()
	// The live Win98 direct-hosted-IPX refusal: header Status 0x00120002 with Flags2
	// NT-status clear = ERRSRV(2)/18, NOT an NTSTATUS (0x00120002's severity bits say
	// "success", which is why it read as nonsense before this was decoded per-reply).
	e := &ErrStatus{Command: CommandNegotiate, Status: 0x00120002, DOS: true}
	class, code := e.ErrorClass()
	if class != ErrClassSrv || code != ErrSrvUnknownName {
		t.Fatalf("ErrorClass = %d/%d, want %d/%d (ERRSRV/18)",
			class, code, ErrClassSrv, ErrSrvUnknownName)
	}
	if got, want := e.Error(), "ERRSRV/unknown-name (2/18)"; !strings.Contains(got, want) {
		t.Errorf("Error() = %q, want it to contain %q", got, want)
	}
	// An NT-status reply keeps the raw hex form.
	nt := &ErrStatus{Command: CommandNegotiate, Status: 0xC000006D}
	if got, want := nt.Error(), "0xC000006D"; !strings.Contains(got, want) {
		t.Errorf("NTSTATUS Error() = %q, want it to contain %q", got, want)
	}
}
