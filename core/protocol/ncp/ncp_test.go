package ncp

import (
	"bytes"
	"testing"
)

func TestUnmarshalRequest_OrdinaryRequest(t *testing.T) {
	// type=0x2222 seq=0x07 connLow=0x05 task=0x01 connHigh=0x00 fn=0x17 body=...
	raw := []byte{0x22, 0x22, 0x07, 0x05, 0x01, 0x00, 0x17, 0x01, 0x02}
	h, err := UnmarshalRequest(raw)
	if err != nil {
		t.Fatalf("UnmarshalRequest: %v", err)
	}
	if h.Type != TypeRequest {
		t.Errorf("Type = %#x, want %#x", h.Type, TypeRequest)
	}
	if h.SequenceNumber != 0x07 || h.TaskNumber != 0x01 {
		t.Errorf("seq/task = %d/%d, want 7/1", h.SequenceNumber, h.TaskNumber)
	}
	if got := h.ConnectionNumber(); got != 5 {
		t.Errorf("ConnectionNumber = %d, want 5", got)
	}
	if h.Function != 0x17 {
		t.Errorf("Function = %#x, want 0x17", h.Function)
	}
	if !bytes.Equal(h.Body, []byte{0x01, 0x02}) {
		t.Errorf("Body = %v, want [1 2]", h.Body)
	}
}

func TestUnmarshalRequest_CreateConnectionHasNoFunction(t *testing.T) {
	raw := []byte{0x11, 0x11, 0x00, 0x00, 0x00, 0x00}
	h, err := UnmarshalRequest(raw)
	if err != nil {
		t.Fatalf("UnmarshalRequest: %v", err)
	}
	if h.Type != TypeCreateConnection {
		t.Errorf("Type = %#x, want create-connection", h.Type)
	}
	if h.Function != 0 || h.Body != nil {
		t.Errorf("create-connection carried function/body: %#x %v", h.Function, h.Body)
	}
}

func TestUnmarshalRequest_Short(t *testing.T) {
	if _, err := UnmarshalRequest([]byte{0x22, 0x22}); err != ErrShort {
		t.Errorf("err = %v, want ErrShort", err)
	}
}

func TestReplyHeaderMarshal(t *testing.T) {
	req := &RequestHeader{SequenceNumber: 0x07, TaskNumber: 0x01}
	r := Reply(req, 5, CompletionSuccess)
	out := r.Marshal(nil)
	if len(out) != ReplyHeaderLen {
		t.Fatalf("reply len = %d, want %d", len(out), ReplyHeaderLen)
	}
	want := []byte{0x33, 0x33, 0x07, 0x05, 0x01, 0x00, 0x00, 0x00}
	if !bytes.Equal(out, want) {
		t.Errorf("reply = %v, want %v", out, want)
	}
}

func TestSAPResponseRoundTrip(t *testing.T) {
	e := SAPEntry{
		Type:    SAPServerTypeFileServer,
		Name:    "OMNITALK",
		Network: [4]byte{0, 0, 0, 0x10},
		Node:    [6]byte{1, 2, 3, 4, 5, 6},
		Socket:  NCPSocket,
		Hops:    1,
	}
	out := MarshalResponse(SAPGeneralResponse, []SAPEntry{e}, nil)
	if len(out) != 2+SAPEntryLen {
		t.Fatalf("SAP len = %d, want %d", len(out), 2+SAPEntryLen)
	}
	q, err := UnmarshalSAPQuery(out)
	if err != nil {
		t.Fatalf("UnmarshalSAPQuery: %v", err)
	}
	if q.Operation != SAPGeneralResponse {
		t.Errorf("op = %#x, want general response", q.Operation)
	}
	// The name field is NUL-padded to 48 bytes starting at offset 4.
	if got := string(bytes.TrimRight(out[4:4+sapNameLen], "\x00")); got != "OMNITALK" {
		t.Errorf("name = %q, want OMNITALK", got)
	}
}

func TestSAPQueryWantsType(t *testing.T) {
	q := &SAPQuery{Operation: SAPNearestQuery, ServiceType: SAPServerTypeFileServer}
	if !q.WantsType(SAPServerTypeFileServer) {
		t.Error("file-server query should want file-server type")
	}
	wild := &SAPQuery{ServiceType: SAPServerTypeWildcard}
	if !wild.WantsType(SAPServerTypeFileServer) {
		t.Error("wildcard query should want any type")
	}
	if q.WantsType(0x0007) {
		t.Error("file-server query should not want print-server type")
	}
}
