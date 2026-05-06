package netbeui

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestNonSessionRoundTrip(t *testing.T) {
	// ADD_NAME_QUERY (0x01) — spec Table 5-12: fixed length 0x002C (44).
	f := &Frame{
		Command:       CmdAddNameQuery,
		Data1:         0x00,
		Data2:         0x0000,
		RspCorrelator: 0x1234,
	}
	copy(f.SourceName[:], []byte("TESTSERVER\x20\x20\x20\x20\x20\x00"))

	encoded, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(encoded) != NonSessionHeaderLength {
		t.Fatalf("expected %d bytes, got %d", NonSessionHeaderLength, len(encoded))
	}
	// Verify wire length field
	wireLen := binary.LittleEndian.Uint16(encoded[0:2])
	if wireLen != uint16(NonSessionHeaderLength) {
		t.Fatalf("wire length = 0x%04X, want 0x%04X", wireLen, NonSessionHeaderLength)
	}
	// Verify delimiter
	delim := binary.LittleEndian.Uint16(encoded[2:4])
	if delim != NBFDelimiter {
		t.Fatalf("delimiter = 0x%04X, want 0x%04X", delim, NBFDelimiter)
	}
	// Verify command
	if encoded[4] != CmdAddNameQuery {
		t.Fatalf("command = 0x%02X, want 0x%02X", encoded[4], CmdAddNameQuery)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Command != f.Command {
		t.Errorf("Command = 0x%02X, want 0x%02X", decoded.Command, f.Command)
	}
	if decoded.RspCorrelator != f.RspCorrelator {
		t.Errorf("RspCorrelator = 0x%04X, want 0x%04X", decoded.RspCorrelator, f.RspCorrelator)
	}
	if decoded.SourceName != f.SourceName {
		t.Errorf("SourceName mismatch")
	}
	if decoded.Payload != nil {
		t.Errorf("expected nil payload, got %d bytes", len(decoded.Payload))
	}
}

func TestSessionRoundTrip(t *testing.T) {
	// DATA_ONLY_LAST (0x16) — spec Table 5-25: length 0x000E (14) + data.
	payload := []byte("Hello, NetBEUI!")
	f := &Frame{
		Command:        CmdDataOnlyLast,
		Data1:          0x00,
		XmitCorrelator: 0xABCD,
		RspCorrelator:  0x5678,
		DestNumber:     0x01,
		SourceNumber:   0x02,
		Payload:        payload,
	}

	encoded, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	expectedLen := SessionHeaderLength + len(payload)
	if len(encoded) != expectedLen {
		t.Fatalf("expected %d bytes, got %d", expectedLen, len(encoded))
	}
	// Verify wire length field
	wireLen := binary.LittleEndian.Uint16(encoded[0:2])
	if wireLen != uint16(expectedLen) {
		t.Fatalf("wire length = %d, want %d", wireLen, expectedLen)
	}
	// Verify session numbers at offsets 12–13
	if encoded[12] != 0x01 || encoded[13] != 0x02 {
		t.Fatalf("session nums = %02X/%02X, want 01/02", encoded[12], encoded[13])
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Command != CmdDataOnlyLast {
		t.Errorf("Command = 0x%02X, want 0x%02X", decoded.Command, CmdDataOnlyLast)
	}
	if decoded.DestNumber != 0x01 {
		t.Errorf("DestNumber = %d, want 1", decoded.DestNumber)
	}
	if decoded.SourceNumber != 0x02 {
		t.Errorf("SourceNumber = %d, want 2", decoded.SourceNumber)
	}
	if decoded.XmitCorrelator != 0xABCD {
		t.Errorf("XmitCorrelator = 0x%04X, want 0xABCD", decoded.XmitCorrelator)
	}
	if !bytes.Equal(decoded.Payload, payload) {
		t.Errorf("payload mismatch: got %q", decoded.Payload)
	}
}

func TestDataAckMinimal(t *testing.T) {
	// DATA_ACK (0x14) — spec Table 5-23: exactly 14 bytes, no payload.
	f := &Frame{
		Command:        CmdDataAck,
		XmitCorrelator: 0x0042,
		DestNumber:     0x03,
		SourceNumber:   0x04,
	}

	encoded, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(encoded) != SessionHeaderLength {
		t.Fatalf("expected %d bytes, got %d", SessionHeaderLength, len(encoded))
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Command != CmdDataAck {
		t.Errorf("Command = 0x%02X, want 0x%02X", decoded.Command, CmdDataAck)
	}
	if decoded.DestNumber != 0x03 || decoded.SourceNumber != 0x04 {
		t.Errorf("session nums = %d/%d, want 3/4", decoded.DestNumber, decoded.SourceNumber)
	}
	if decoded.Payload != nil {
		t.Errorf("expected nil payload")
	}
}

func TestNameInConflictRoundTrip(t *testing.T) {
	// NAME_IN_CONFLICT (0x02) — spec Table 5-13: 44 bytes.
	conflictName := [16]byte{}
	copy(conflictName[:], "CONFLICT\x20\x20\x20\x20\x20\x20\x20\x00")

	// Source = NAME_NUMBER_1: 10 zero bytes + 6 MAC bytes
	srcName := [16]byte{}
	copy(srcName[10:], []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF})

	f := &Frame{
		Command:         CmdNameInConflict,
		DestinationName: conflictName,
		SourceName:      srcName,
	}

	encoded, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if len(encoded) != NonSessionHeaderLength {
		t.Fatalf("expected %d bytes, got %d", NonSessionHeaderLength, len(encoded))
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.DestinationName != conflictName {
		t.Errorf("DestinationName mismatch")
	}
	if decoded.SourceName != srcName {
		t.Errorf("SourceName mismatch")
	}
}

func TestStatusResponseWithPayload(t *testing.T) {
	// STATUS_RESPONSE (0x0F) — non-session with status data payload.
	statusData := make([]byte, 60)
	for i := range statusData {
		statusData[i] = byte(i)
	}
	f := &Frame{
		Command:        CmdStatusResponse,
		Data1:          0x01, // NetBIOS 2.1
		XmitCorrelator: 0x9999,
		Payload:        statusData,
	}
	copy(f.SourceName[:], "REMOTE\x20\x20\x20\x20\x20\x20\x20\x20\x20\x00")

	encoded, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	expectedLen := NonSessionHeaderLength + len(statusData)
	if len(encoded) != expectedLen {
		t.Fatalf("expected %d bytes, got %d", expectedLen, len(encoded))
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(decoded.Payload, statusData) {
		t.Errorf("payload mismatch")
	}
}

func TestDecodeShortFrame(t *testing.T) {
	_, err := Decode([]byte{0x00, 0x01})
	if err != ErrShortFrame {
		t.Fatalf("expected ErrShortFrame, got %v", err)
	}
}

func TestDecodeBadDelimiter(t *testing.T) {
	b := make([]byte, NonSessionHeaderLength)
	binary.LittleEndian.PutUint16(b[0:2], NonSessionHeaderLength)
	binary.LittleEndian.PutUint16(b[2:4], 0xBEEF) // wrong delimiter
	b[4] = CmdAddNameQuery
	_, err := Decode(b)
	if err != ErrBadDelimiter {
		t.Fatalf("expected ErrBadDelimiter, got %v", err)
	}
}

func TestDecodeSessionShortFrame(t *testing.T) {
	// Provide valid common prefix but too short for session header.
	b := make([]byte, commonPrefixLen)
	binary.LittleEndian.PutUint16(b[0:2], uint16(SessionHeaderLength))
	binary.LittleEndian.PutUint16(b[2:4], NBFDelimiter)
	b[4] = CmdDataAck // session command

	_, err := Decode(b)
	if err != ErrShortFrame {
		t.Fatalf("expected ErrShortFrame, got %v", err)
	}
}

func TestBackwardCompatResponseCorrelator(t *testing.T) {
	// Verify the deprecated ResponseCorrelator alias works.
	f := &Frame{
		Command:            CmdAddNameQuery,
		ResponseCorrelator: 0x4321,
	}

	encoded, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.RspCorrelator != 0x4321 {
		t.Errorf("RspCorrelator = 0x%04X, want 0x4321", decoded.RspCorrelator)
	}
	if decoded.ResponseCorrelator != 0x4321 {
		t.Errorf("ResponseCorrelator alias = 0x%04X, want 0x4321", decoded.ResponseCorrelator)
	}
}

func TestIsSessionCommand(t *testing.T) {
	nonSession := []uint8{
		CmdAddGroupNameQuery, CmdAddNameQuery, CmdNameInConflict,
		CmdStatusQuery, CmdTerminateTraceRemote, CmdDatagram,
		CmdDatagramBroadcast, CmdNameQuery, CmdAddNameResponse,
		CmdNameRecognized, CmdStatusResponse, CmdTerminateTraceLocal,
	}
	for _, cmd := range nonSession {
		if IsSessionCommand(cmd) {
			t.Errorf("IsSessionCommand(0x%02X) = true, want false", cmd)
		}
	}

	session := []uint8{
		CmdDataAck, CmdDataFirstMiddle, CmdDataOnlyLast,
		CmdSessionConfirm, CmdSessionEnd, CmdSessionInitialize,
		CmdNoReceive, CmdReceiveOutstanding, CmdReceiveContinue,
		CmdSessionAlive,
	}
	for _, cmd := range session {
		if !IsSessionCommand(cmd) {
			t.Errorf("IsSessionCommand(0x%02X) = false, want true", cmd)
		}
	}
}
