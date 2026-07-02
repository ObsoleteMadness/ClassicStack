//go:build afp || all

package afp

import (
	"context"
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
)

// TestASP_DuplicateCommandDropped proves the per-session ASP duplicate filter:
// a retransmitted command (same ASP seqNum under a DIFFERENT ATP tid) must be
// dropped, not re-executed. Without it a client's ATP-level retransmission would
// run a non-idempotent command (here FPCreateFile) twice — the second run seeing
// the object it just created and returning kFPObjectExists.
func TestASP_DuplicateCommandDropped(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}
	sessID := login(t, svc, r)

	r.reset()
	openVol := []byte{cmdOpenVol, 0}
	openVol = bp.AppendBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 3), openVol)), from)
	volID := bp.BE16(respPayload(r.lastReply())[2:4])

	// FPCreateFile "new.txt" at the root, ASP seqNum 4, ATP tid 100.
	create := []byte{cmdCreateFile, 0}
	create = bp.AppendBE16(create, volID)
	create = bp.AppendBE32(create, 2) // dirID root
	create = append(create, PathTypeUTF8Names)
	create = putPString(create, []byte("new.txt"))

	r.reset()
	svc.Inbound(ddpTo(svc.Socket(), atpTReqTID(aspUserData(asp.SPFuncCommand, sessID, 4), 100, create)), from)
	if got := int32(respUserData(r.lastReply())); got != afpNoErr {
		t.Fatalf("first FPCreateFile = %d, want 0", got)
	}

	// The workstation retransmits the SAME ASP request (seqNum 4) under a fresh
	// ATP tid (101): the duplicate filter must drop it — no reply, and no second
	// create (which would have returned kFPObjectExists).
	r.reset()
	svc.Inbound(ddpTo(svc.Socket(), atpTReqTID(aspUserData(asp.SPFuncCommand, sessID, 4), 101, create)), from)
	if len(r.replies) != 0 {
		t.Fatalf("duplicate command produced %d replies, want 0 (must be silently dropped)", len(r.replies))
	}

	// A genuinely new command (fresh seqNum) is still processed: creating the same
	// file now correctly reports it exists, proving the first create landed and the
	// session is still live.
	r.reset()
	svc.Inbound(ddpTo(svc.Socket(), atpTReqTID(aspUserData(asp.SPFuncCommand, sessID, 5), 102, create)), from)
	if got := int32(respUserData(r.lastReply())); got != afpErrObjectExists {
		t.Fatalf("re-create with fresh seq = %d, want kFPObjectExists (-5017)", got)
	}
}

// TestASP_OversizedCommandRejected proves a command block larger than one ATP
// packet is rejected with SPErrorSizeErr rather than processed.
func TestASP_OversizedCommandRejected(t *testing.T) {
	svc, r := newRunningService(t)
	from := fakePort{}
	sessID := login(t, svc, r)

	// A command block of ATPMaxData+1 bytes (command byte + padding).
	big := make([]byte, asp.ATPMaxData+1)
	big[0] = cmdGetSrvrParms

	r.reset()
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(aspUserData(asp.SPFuncCommand, sessID, 4), big)), from)
	if got := int32(respUserData(r.lastReply())); got != int32(asp.SPErrorSizeErr) {
		t.Fatalf("oversized command result = %d, want SPErrorSizeErr (%d)", got, asp.SPErrorSizeErr)
	}
}

// TestASP_StopSendsServerGoingDown proves Stop notifies every live session with an
// SPAttention(ServerGoingDown) before tearing down.
func TestASP_StopSendsServerGoingDown(t *testing.T) {
	svc, r := newRunningService(t)
	login(t, svc, r) // one live session

	r.reset()
	if err := svc.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Exactly one live session → exactly one SPAttention routed to it.
	var sawAttn bool
	for _, d := range r.routed {
		h, err := atp.Decode(d.Data)
		if err != nil {
			continue
		}
		if h.FuncCode() != atp.FuncTReq {
			continue
		}
		if uint8(h.UserData>>24) == asp.SPFuncAttention {
			sawAttn = true
			if code := uint16(h.UserData); code != asp.AspAttnServerGoingDown {
				t.Errorf("attention code = %#x, want ServerGoingDown %#x", code, asp.AspAttnServerGoingDown)
			}
			if d.DestSocket != 200 {
				t.Errorf("attention DestSocket = %d, want 200 (WSS)", d.DestSocket)
			}
		}
	}
	if !sawAttn {
		t.Fatalf("Stop did not send ServerGoingDown attention to the live session")
	}
}
