//go:build afp || all

package asp

import (
	"encoding/binary"
	"testing"

	"github.com/ObsoleteMadness/ClassicStack/service/atp"
)

type stubCommandHandler struct {
	status []byte
	reply  []byte
	err    int32
}

func (h stubCommandHandler) HandleCommand(_ []byte) ([]byte, int32) {
	return append([]byte(nil), h.reply...), h.err
}

func (h stubCommandHandler) GetStatus() []byte {
	return append([]byte(nil), h.status...)
}

func TestHandleCommandUnknownSessionReturnsParamErr(t *testing.T) {
	s := New("test", nil, nil, nil)
	s.quantumSize = QuantumSize

	in := atp.IncomingRequest{
		UserBytes: (uint32(SPFuncCommand) << 24) | (uint32(42) << 16),
		Data:      []byte{0x01},
		Bitmap:    0x01,
	}

	var got atp.ResponseMessage
	s.handleCommand(in, func(m atp.ResponseMessage) { got = m })

	if len(got.UserBytes) != 1 || got.UserBytes[0] != errToUserBytes(SPErrorParamErr) {
		t.Fatalf("expected ParamErr user bytes, got %#v", got.UserBytes)
	}
}

func TestHandleCloseSessionUnknownSessionReturnsParamErr(t *testing.T) {
	s := New("test", nil, nil, nil)

	in := atp.IncomingRequest{
		UserBytes: (uint32(SPFuncCloseSess) << 24) | (uint32(99) << 16),
	}

	var got atp.ResponseMessage
	s.handleCloseSession(in, func(m atp.ResponseMessage) { got = m })

	if len(got.UserBytes) != 1 || got.UserBytes[0] != errToUserBytes(SPErrorParamErr) {
		t.Fatalf("expected ParamErr user bytes, got %#v", got.UserBytes)
	}
}

func TestHandleCommandReplyOverQuantumGetsTruncated(t *testing.T) {
	// Per AFP spec: FPRead, FPWrite, FPEnumerate can return partially.
	// When reply exceeds QuantumSize, ASP should truncate and preserve the
	// original AFP error code, allowing workstation to make additional requests.
	h := stubCommandHandler{reply: make([]byte, 12), err: SPErrorNoError}
	s := New("test", h, nil, nil)
	s.quantumSize = 8
	s.sm.Open(1, 1, 1, 1, 1)

	in := atp.IncomingRequest{
		UserBytes: (uint32(SPFuncCommand) << 24) | (uint32(1) << 16) | 1,
		Data:      []byte{0x01},
		Bitmap:    0xFF,
		TID:       1,
	}

	var got atp.ResponseMessage
	s.handleCommand(in, func(m atp.ResponseMessage) { got = m })

	// Should preserve the NoError code and truncate to quantum size
	if len(got.UserBytes) != 1 || got.UserBytes[0] != errToUserBytes(SPErrorNoError) {
		t.Fatalf("expected NoError user bytes, got %#v", got.UserBytes)
	}
	// Check that data was truncated to quantum size
	totalReplyLen := 0
	for _, buf := range got.Buffers {
		totalReplyLen += len(buf)
	}
	if totalReplyLen > 8 {
		t.Fatalf("reply %d bytes exceeds quantum size 8", totalReplyLen)
	}
}

func TestHandleGetStatusOverQuantumReturnsSizeErr(t *testing.T) {
	h := stubCommandHandler{status: make([]byte, 10)}
	s := New("test", h, nil, nil)
	s.quantumSize = 8

	in := atp.IncomingRequest{Bitmap: 0xFF}

	var got atp.ResponseMessage
	s.handleGetStatus(in, func(m atp.ResponseMessage) { got = m })

	if len(got.UserBytes) != 1 || got.UserBytes[0] != errToUserBytes(SPErrorSizeErr) {
		t.Fatalf("expected SizeErr user bytes, got %#v", got.UserBytes)
	}
}

func TestHandleCommandCmdBlockOverMaxReturnsSizeErr(t *testing.T) {
	s := New("test", nil, nil, nil)
	s.maxCmdSize = 4
	s.quantumSize = QuantumSize

	in := atp.IncomingRequest{
		UserBytes: (uint32(SPFuncCommand) << 24) | (uint32(1) << 16),
		Data:      []byte{1, 2, 3, 4, 5},
		Bitmap:    0x01,
	}

	var got atp.ResponseMessage
	s.handleCommand(in, func(m atp.ResponseMessage) { got = m })

	if len(got.UserBytes) != 1 || got.UserBytes[0] != errToUserBytes(SPErrorSizeErr) {
		t.Fatalf("expected SizeErr user bytes, got %#v", got.UserBytes)
	}
}

func TestHandleCommandReplyOverWorkstationCapacityGetsTruncated(t *testing.T) {
	h := stubCommandHandler{reply: make([]byte, ATPMaxData+10), err: SPErrorNoError}
	s := New("test", h, nil, nil)
	s.maxCmdSize = ATPMaxData
	s.quantumSize = QuantumSize
	s.sm.Open(1, 1, 1, 1, 1)

	in := atp.IncomingRequest{
		UserBytes: (uint32(SPFuncCommand) << 24) | (uint32(1) << 16) | 1,
		Data:      []byte{0x01},
		Bitmap:    0x01,
		TID:       1,
	}

	var got atp.ResponseMessage
	s.handleCommand(in, func(m atp.ResponseMessage) { got = m })

	if len(got.UserBytes) != 1 || got.UserBytes[0] != errToUserBytes(SPErrorNoError) {
		t.Fatalf("expected NoError user bytes, got %#v", got.UserBytes)
	}
	totalReplyLen := 0
	for _, buf := range got.Buffers {
		totalReplyLen += len(buf)
	}
	if totalReplyLen > ATPMaxData {
		t.Fatalf("reply %d bytes exceeds bitmap capacity %d", totalReplyLen, ATPMaxData)
	}
}

func TestHandleWriteCmdBlockOverMaxReturnsSizeErr(t *testing.T) {
	s := New("test", nil, nil, nil)
	s.maxCmdSize = 4
	s.quantumSize = QuantumSize

	in := atp.IncomingRequest{
		UserBytes: (uint32(SPFuncWrite) << 24) | (uint32(1) << 16),
		Data:      []byte{1, 2, 3, 4, 5},
		Bitmap:    0x01,
	}

	var got atp.ResponseMessage
	s.handleASPWrite(in, func(m atp.ResponseMessage) { got = m })

	if len(got.UserBytes) != 1 || got.UserBytes[0] != errToUserBytes(SPErrorSizeErr) {
		t.Fatalf("expected SizeErr user bytes, got %#v", got.UserBytes)
	}
}

func TestHandleWriteNegativeBufferSizeReturnsParamErr(t *testing.T) {
	s := New("test", nil, nil, nil)
	s.maxCmdSize = ATPMaxData
	s.quantumSize = QuantumSize
	s.sm.Open(1, 1, 1, 1, 1)

	cmd := make([]byte, 12)
	binary.BigEndian.PutUint32(cmd[8:12], uint32(0xFFFFFFFF))

	in := atp.IncomingRequest{
		UserBytes: (uint32(SPFuncWrite) << 24) | (uint32(1) << 16) | 1,
		Data:      cmd,
		Bitmap:    0x01,
		TID:       1,
		Src:       atp.Address{Net: 1, Node: 1, Socket: 1},
		Local:     atp.Address{Net: 1, Node: 2, Socket: ServerSocket},
	}

	var got atp.ResponseMessage
	s.handleASPWrite(in, func(m atp.ResponseMessage) { got = m })

	if len(got.UserBytes) != 1 || got.UserBytes[0] != errToUserBytes(SPErrorParamErr) {
		t.Fatalf("expected ParamErr user bytes, got %#v", got.UserBytes)
	}
}
