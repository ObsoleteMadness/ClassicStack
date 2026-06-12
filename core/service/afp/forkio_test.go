package afp

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
)

// openVolForFork logs in and opens "Share", returning the session id and volume id
// so a fork test can address commands at an open volume.
func openVolForFork(t *testing.T, svc *Service, r *fakeRouter) (sessID uint8, volID uint16) {
	t.Helper()
	from := fakePort{}
	sessID = login(t, svc, r)
	r.reset()
	openVol := []byte{cmdOpenVol, 0}
	openVol = bp.AppendBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("OpenVol result = %d, want 0", got)
	}
	return sessID, bp.BE16(respPayload(r.lastReply())[2:4])
}

// sendCmd issues one AFP command block on a session and returns the result code
// and reply payload.
func sendCmd(t *testing.T, svc *Service, r *fakeRouter, sessID uint8, seq uint16, block []byte) (int32, []byte) {
	t.Helper()
	r.reset()
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, seq), block)), fakePort{})
	return int32(respUserData(r.lastReply())), respPayload(r.lastReply())
}

// TestForkIO_OpenWriteReadClose drives the data-fork round trip end-to-end over
// the dispatch spine: FPOpenFork(R/W) → FPWrite → FPRead → FPCloseFork, proving
// the fork handle reaches storage through the fork engine and positional I/O.
func TestForkIO_OpenWriteReadClose(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "doc.txt") // seeds "data" (4 bytes)

	sessID, volID := openVolForFork(t, svc, r)

	// FPOpenFork data fork, read/write.
	openFork := []byte{cmdOpenFork, forkFlagData}
	openFork = bp.AppendBE16(openFork, volID)
	openFork = bp.AppendBE32(openFork, 2) // dirID root
	openFork = bp.AppendBE16(openFork, fileBitmapDataForkLen)
	openFork = bp.AppendBE16(openFork, accessRead|accessWrite)
	openFork = append(openFork, PathTypeUTF8Names)
	openFork = append(openFork, []byte("doc.txt")...)
	code, reply := sendCmd(t, svc, r, sessID, 4, openFork)
	if code != afpNoErr {
		t.Fatalf("OpenFork result = %d, want 0", code)
	}
	// reply = bitmap(2) forkRef(2) <params: dataForkLen(4)>.
	forkRef := bp.BE16(reply[2:4])
	if forkRef == 0 {
		t.Fatal("OpenFork returned fork ref 0")
	}
	if gotLen := bp.BE32(reply[4:8]); gotLen != 4 {
		t.Fatalf("OpenFork dataForkLen = %d, want 4", gotLen)
	}

	// FPWrite "hello world" at offset 0.
	payload := []byte("hello world")
	write := []byte{cmdWrite, 0x00} // flag 0 → offset from start
	write = bp.AppendBE16(write, forkRef)
	write = bp.AppendBE32(write, 0) // offset
	write = bp.AppendBE32(write, uint32(len(payload)))
	write = append(write, payload...)
	code, wreply := sendCmd(t, svc, r, sessID, 5, write)
	if code != afpNoErr {
		t.Fatalf("Write result = %d, want 0", code)
	}
	if last := bp.BE32(wreply[0:4]); last != uint32(len(payload)) {
		t.Fatalf("Write lastWritten = %d, want %d", last, len(payload))
	}

	// FPRead the bytes back.
	read := []byte{cmdRead, 0x00}
	read = bp.AppendBE16(read, forkRef)
	read = bp.AppendBE32(read, 0)                    // offset
	read = bp.AppendBE32(read, uint32(len(payload))) // reqCount
	code, got := sendCmd(t, svc, r, sessID, 6, read)
	if code != afpNoErr {
		t.Fatalf("Read result = %d, want 0 (got %d)", code, code)
	}
	if string(got) != string(payload) {
		t.Fatalf("Read = %q, want %q", got, payload)
	}

	// A read past end-of-fork returns kFPEOFErr.
	readPast := []byte{cmdRead, 0x00}
	readPast = bp.AppendBE16(readPast, forkRef)
	readPast = bp.AppendBE32(readPast, uint32(len(payload))) // offset == fork length
	readPast = bp.AppendBE32(readPast, 16)
	code, _ = sendCmd(t, svc, r, sessID, 7, readPast)
	if code != afpErrEOFErr {
		t.Fatalf("Read past EOF result = %d, want %d", code, afpErrEOFErr)
	}

	// FPCloseFork.
	closeFork := []byte{cmdCloseFork, 0}
	closeFork = bp.AppendBE16(closeFork, forkRef)
	code, _ = sendCmd(t, svc, r, sessID, 8, closeFork)
	if code != afpNoErr {
		t.Fatalf("CloseFork result = %d, want 0", code)
	}
	// The fork ref is now invalid.
	code, _ = sendCmd(t, svc, r, sessID, 9, read)
	if code != afpErrParamErr {
		t.Fatalf("Read after close result = %d, want %d", code, afpErrParamErr)
	}
}

// TestForkIO_WriteFromEnd proves the FPWrite "from end" flag appends at the
// current fork length rather than at the literal offset.
func TestForkIO_WriteFromEnd(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "log.txt") // "data"

	sessID, volID := openVolForFork(t, svc, r)

	openFork := []byte{cmdOpenFork, forkFlagData}
	openFork = bp.AppendBE16(openFork, volID)
	openFork = bp.AppendBE32(openFork, 2)
	openFork = bp.AppendBE16(openFork, 0)
	openFork = bp.AppendBE16(openFork, accessRead|accessWrite)
	openFork = append(openFork, PathTypeUTF8Names)
	openFork = append(openFork, []byte("log.txt")...)
	code, reply := sendCmd(t, svc, r, sessID, 4, openFork)
	if code != afpNoErr {
		t.Fatalf("OpenFork result = %d, want 0", code)
	}
	forkRef := bp.BE16(reply[2:4])

	// Append " more" from end; the file already holds "data" (4 bytes).
	more := []byte(" more")
	write := []byte{cmdWrite, fromEndFlag}
	write = bp.AppendBE16(write, forkRef)
	write = bp.AppendBE32(write, 0) // offset 0 from end → append
	write = bp.AppendBE32(write, uint32(len(more)))
	write = append(write, more...)
	code, wreply := sendCmd(t, svc, r, sessID, 5, write)
	if code != afpNoErr {
		t.Fatalf("Write(fromEnd) result = %d, want 0", code)
	}
	if last := bp.BE32(wreply[0:4]); last != uint32(4+len(more)) {
		t.Fatalf("Write(fromEnd) lastWritten = %d, want %d", last, 4+len(more))
	}

	// Read the whole fork back: "data more".
	read := []byte{cmdRead, 0x00}
	read = bp.AppendBE16(read, forkRef)
	read = bp.AppendBE32(read, 0)
	read = bp.AppendBE32(read, 9)
	code, got := sendCmd(t, svc, r, sessID, 6, read)
	if code != afpNoErr {
		t.Fatalf("Read result = %d, want 0", code)
	}
	if string(got) != "data more" {
		t.Fatalf("Read = %q, want %q", got, "data more")
	}
}

// TestForkIO_WriteToReadOnlyFork proves a write to a fork opened read-only is
// rejected with kFPAccessDenied rather than corrupting the file.
func TestForkIO_WriteToReadOnlyFork(t *testing.T) {
	svc, r := newRunningService(t)
	vol := svc.Volumes()[0]
	mustCreate(t, vol, "ro.txt")

	sessID, volID := openVolForFork(t, svc, r)

	openFork := []byte{cmdOpenFork, forkFlagData}
	openFork = bp.AppendBE16(openFork, volID)
	openFork = bp.AppendBE32(openFork, 2)
	openFork = bp.AppendBE16(openFork, 0)
	openFork = bp.AppendBE16(openFork, accessRead) // read-only
	openFork = append(openFork, PathTypeUTF8Names)
	openFork = append(openFork, []byte("ro.txt")...)
	code, reply := sendCmd(t, svc, r, sessID, 4, openFork)
	if code != afpNoErr {
		t.Fatalf("OpenFork result = %d, want 0", code)
	}
	forkRef := bp.BE16(reply[2:4])

	write := []byte{cmdWrite, 0x00}
	write = bp.AppendBE16(write, forkRef)
	write = bp.AppendBE32(write, 0)
	write = bp.AppendBE32(write, 3)
	write = append(write, []byte("no!")...)
	code, _ = sendCmd(t, svc, r, sessID, 5, write)
	if code != afpErrAccessDenied {
		t.Fatalf("Write to R/O fork result = %d, want %d", code, afpErrAccessDenied)
	}
}
