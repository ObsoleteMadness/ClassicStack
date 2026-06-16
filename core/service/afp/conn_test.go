package afp

import (
	"testing"

	bp "github.com/ObsoleteMadness/ClassicStack/core/binaryprimitives"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// newSeamService builds an AFP service with one memfs volume but NO router and NO
// ASP layer — the command core must work driven only through the conn.go seam,
// which is the whole point of the §3-bis extraction (a future DSI transport drives
// the same Conn with no ASP/ATP/DDP in sight).
func newSeamService(t *testing.T) *Service {
	t.Helper()
	svc, err := NewWithVolumes(nil, VolumeSpec{
		ID:   1,
		Name: "Share",
		Share: fs.ShareSpec{
			Name: "Share", FSType: "memfs",
			ForkBackend: "appledouble", FilenameCodec: "macroman-utf8",
		},
	})
	if err != nil {
		t.Fatalf("NewWithVolumes: %v", err)
	}
	return svc
}

// guestLoginBlock is the FPLogin command block for a No-User-Authent (guest) login,
// as Conn.Command expects it (command byte + arguments, no ASP/ATP framing).
func guestLoginBlock() []byte {
	block := []byte{cmdLogin}
	block = putPString(block, []byte("AFP2.2"))
	block = putPString(block, []byte("No User Authent"))
	return block
}

// TestConn_GetServerInfoSessionless proves the one sessionless seam call works
// without opening a circuit (the ASPGetStatus / DSIGetStatus path).
func TestConn_GetServerInfoSessionless(t *testing.T) {
	svc := newSeamService(t)
	h := HandlerAdapter{Service: svc}

	block := h.GetServerInfo()
	if len(block) == 0 {
		t.Fatal("GetServerInfo returned an empty block")
	}
	// It must be byte-identical to what an FPGetSrvrInfo command returns on a
	// circuit — the same server-info block, two ways in.
	c := h.NewConn()
	reply, result := c.Command([]byte{cmdGetSrvrInfo})
	if result != afpNoErr {
		t.Fatalf("FPGetSrvrInfo result = %d, want 0", result)
	}
	if string(reply) != string(block) {
		t.Fatal("GetServerInfo and FPGetSrvrInfo returned different blocks")
	}
}

// TestConn_LoginGatesCommands proves the command core enforces the login gate over
// the seam: a catalog command before FPLogin is denied, and admitted after.
func TestConn_LoginGatesCommands(t *testing.T) {
	svc := newSeamService(t)
	c := HandlerAdapter{Service: svc}.NewConn()

	// FPGetSrvrParms before login → access denied.
	if _, result := c.Command([]byte{cmdGetSrvrParms}); result != afpErrAccessDenied {
		t.Fatalf("GetSrvrParms before login = %d, want AccessDenied", result)
	}

	if _, result := c.Command(guestLoginBlock()); result != afpNoErr {
		t.Fatalf("FPLogin(guest) result = %d, want 0", result)
	}

	// Now admitted, and the share is listed for the guest identity.
	reply, result := c.Command([]byte{cmdGetSrvrParms})
	if result != afpNoErr {
		t.Fatalf("GetSrvrParms after login = %d, want 0", result)
	}
	if names := volNames(reply); !contains(names, "Share") {
		t.Fatalf("volume list = %v, want to contain Share", names)
	}
}

// TestConn_FullSequenceOverSeam drives login → OpenVol → OpenFork entirely through
// Conn.Command (no router, no ASP), proving the AFP command set is reachable purely
// over the transport-neutral seam.
func TestConn_FullSequenceOverSeam(t *testing.T) {
	svc := newSeamService(t)
	mustCreate(t, svc.Volumes()[0], "doc.txt")
	c := svc.NewConn()

	if _, result := c.Command(guestLoginBlock()); result != afpNoErr {
		t.Fatalf("FPLogin result = %d, want 0", result)
	}

	// FPOpenVol "Share".
	openVol := []byte{cmdOpenVol, 0}
	openVol = bp.AppendBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	reply, result := c.Command(openVol)
	if result != afpNoErr {
		t.Fatalf("OpenVol result = %d, want 0", result)
	}
	volID := bp.BE16(reply[2:4])

	// FPOpenFork the data fork read/write.
	openFork := []byte{cmdOpenFork, forkFlagData}
	openFork = bp.AppendBE16(openFork, volID)
	openFork = bp.AppendBE32(openFork, 2) // dirID root
	openFork = bp.AppendBE16(openFork, fileBitmapDataForkLen)
	openFork = bp.AppendBE16(openFork, accessRead|accessWrite)
	openFork = append(openFork, PathTypeUTF8Names)
	openFork = append(openFork, []byte("doc.txt")...)
	if _, result := c.Command(openFork); result != afpNoErr {
		t.Fatalf("OpenFork result = %d, want 0", result)
	}
}

// TestConn_CloseDrainsForks proves Close releases the circuit's open forks, so a
// transport dropping a circuit (ASP CloseSession, or a lost DSI/TCP connection)
// does not leak file handles — the seam's teardown responsibility.
func TestConn_CloseDrainsForks(t *testing.T) {
	svc := newSeamService(t)
	mustCreate(t, svc.Volumes()[0], "doc.txt")
	c := svc.NewConn()

	if _, result := c.Command(guestLoginBlock()); result != afpNoErr {
		t.Fatalf("FPLogin result = %d, want 0", result)
	}
	openVol := []byte{cmdOpenVol, 0}
	openVol = bp.AppendBE16(openVol, volBitmapID)
	openVol = putPString(openVol, []byte("Share"))
	reply, _ := c.Command(openVol)
	volID := bp.BE16(reply[2:4])

	openFork := []byte{cmdOpenFork, forkFlagData}
	openFork = bp.AppendBE16(openFork, volID)
	openFork = bp.AppendBE32(openFork, 2)
	openFork = bp.AppendBE16(openFork, fileBitmapDataForkLen)
	openFork = bp.AppendBE16(openFork, accessRead|accessWrite)
	openFork = append(openFork, PathTypeUTF8Names)
	openFork = append(openFork, []byte("doc.txt")...)
	if _, result := c.Command(openFork); result != afpNoErr {
		t.Fatalf("OpenFork result = %d, want 0", result)
	}

	// One fork is held (white-box: the seam state lives on the afpSession).
	if n := len(c.afp.forks.byRef); n != 1 {
		t.Fatalf("open forks before Close = %d, want 1", n)
	}
	c.Close()
	if n := len(c.afp.forks.byRef); n != 0 {
		t.Fatalf("open forks after Close = %d, want 0 (leaked)", n)
	}
}

// TestConn_CircuitsAreIndependent proves two circuits on one service do not share
// AFP session state — a login on one does not log in the other.
func TestConn_CircuitsAreIndependent(t *testing.T) {
	svc := newSeamService(t)
	a := svc.NewConn()
	b := svc.NewConn()

	if _, result := a.Command(guestLoginBlock()); result != afpNoErr {
		t.Fatalf("circuit a login result = %d, want 0", result)
	}
	// b never logged in: a catalog command is still denied.
	if _, result := b.Command([]byte{cmdGetSrvrParms}); result != afpErrAccessDenied {
		t.Fatalf("circuit b GetSrvrParms = %d, want AccessDenied (state leaked from a)", result)
	}
}
