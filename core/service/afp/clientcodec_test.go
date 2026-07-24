package afp

// clientcodec_test.go is the ANTI-DRIFT cross-check between the server's own
// request-parse/reply-marshal (this package) and the client-direction DTOs in
// core/protocol/afp: it marshals command blocks with the CLIENT codec, feeds them
// through the full server stack (svc.Inbound → ASP → dispatchAFP), and parses the
// replies with the CLIENT parsers. If either direction's wire layout drifts, one of
// these assertions fails — so the two codecs cannot silently diverge (plan §Verify.1).

import (
	"testing"

	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/asp"
)

// clientCmd runs one client-marshalled AFP command block through the server and
// returns the AFP result code and the reply body.
func clientCmd(t *testing.T, svc *Service, r *fakeRouter, sessID uint8, seq uint16, block []byte) (int32, []byte) {
	t.Helper()
	r.reset()
	ud := aspUserData(asp.SPFuncCommand, sessID, seq)
	svc.Inbound(ddpTo(svc.Socket(), atpTReq(ud, block)), fakePort{})
	if len(r.replies) == 0 {
		t.Fatalf("command %d produced no reply", block[0])
	}
	code := int32(respUserData(r.lastReply()))
	return code, respPayload(r.lastReply())
}

func TestClientCodec_CrossCheck(t *testing.T) {
	svc, r := newRunningService(t)
	sessID := login(t, svc, r)
	seq := uint16(10)
	next := func() uint16 { seq++; return seq }

	// FPGetSrvrParms — the volume list.
	code, body := clientCmd(t, svc, r, sessID, next(), proto.GetSrvrParmsRequest{}.Marshal())
	if code != proto.NoErr {
		t.Fatalf("GetSrvrParms result = %s", proto.ResultName(code))
	}
	sp, ok := proto.ParseGetSrvrParmsReply(body)
	if !ok || len(sp.Volumes) == 0 || sp.Volumes[0].Name != "Share" {
		t.Fatalf("GetSrvrParms reply = %+v ok=%v", sp, ok)
	}

	// FPOpenVol — open "Share", parse the volume params for its id.
	openReq := proto.OpenVolRequest{
		Bitmap:  proto.VolBitmapID | proto.VolBitmapSignature | proto.VolBitmapName,
		VolName: "Share",
	}
	code, body = clientCmd(t, svc, r, sessID, next(), openReq.Marshal())
	if code != proto.NoErr {
		t.Fatalf("OpenVol result = %s", proto.ResultName(code))
	}
	vp, ok := proto.ParseVolParams(body)
	if !ok {
		t.Fatal("ParseVolParams failed")
	}
	if vp.Signature != proto.VolSignatureFixedDirID {
		t.Errorf("volume signature = %d, want FixedDirID", vp.Signature)
	}
	if vp.Name != "Share" {
		t.Errorf("volume name = %q, want Share", vp.Name)
	}
	volID := vp.VolID

	// FPCreateFile at the volume root.
	createReq := proto.CreateFileRequest{
		VolID:    volID,
		DirID:    proto.CNIDRoot,
		PathType: proto.PathTypeLongNames,
		Path:     []byte("hello.txt"),
	}
	code, _ = clientCmd(t, svc, r, sessID, next(), createReq.Marshal())
	if code != proto.NoErr {
		t.Fatalf("CreateFile result = %s", proto.ResultName(code))
	}

	// FPEnumerate the root — the new file must appear, name recovered via the
	// offset-addressed variable area.
	enumReq := proto.EnumerateRequest{
		VolID:        volID,
		DirID:        proto.CNIDRoot,
		FileBitmap:   proto.FDBitmapLongName | proto.FileBitmapDataForkLen,
		DirBitmap:    proto.FDBitmapLongName,
		ReqCount:     20,
		StartIndex:   1,
		MaxReplySize: 4000,
		PathType:     proto.PathTypeLongNames,
		Path:         nil,
	}
	code, body = clientCmd(t, svc, r, sessID, next(), enumReq.Marshal())
	if code != proto.NoErr {
		t.Fatalf("Enumerate result = %s", proto.ResultName(code))
	}
	er, ok := proto.ParseEnumerateReply(body)
	if !ok {
		t.Fatal("ParseEnumerateReply failed")
	}
	var foundFile bool
	for _, e := range er.Entries {
		if string(e.LongName) == "hello.txt" {
			foundFile = true
		}
	}
	if !foundFile {
		t.Fatalf("hello.txt not in enumeration; entries=%d", len(er.Entries))
	}

	// FPGetFileDirParms on the file — the client parser recovers IsDir=false.
	gfdReq := proto.GetFileDirParmsRequest{
		VolID:      volID,
		DirID:      proto.CNIDRoot,
		FileBitmap: proto.FDBitmapLongName | proto.FileBitmapDataForkLen,
		DirBitmap:  proto.FDBitmapLongName,
		PathType:   proto.PathTypeLongNames,
		Path:       []byte("hello.txt"),
	}
	code, body = clientCmd(t, svc, r, sessID, next(), gfdReq.Marshal())
	if code != proto.NoErr {
		t.Fatalf("GetFileDirParms result = %s", proto.ResultName(code))
	}
	gr, ok := proto.ParseGetFileDirParmsReply(body)
	if !ok || gr.IsDir {
		t.Fatalf("GetFileDirParms reply IsDir=%v ok=%v", gr.IsDir, ok)
	}
	if string(gr.Params.LongName) != "hello.txt" {
		t.Errorf("GetFileDirParms name = %q, want hello.txt", gr.Params.LongName)
	}

	// FPDelete the file.
	delReq := proto.DeleteRequest{
		VolID:    volID,
		DirID:    proto.CNIDRoot,
		PathType: proto.PathTypeLongNames,
		Path:     []byte("hello.txt"),
	}
	code, _ = clientCmd(t, svc, r, sessID, next(), delReq.Marshal())
	if code != proto.NoErr {
		t.Fatalf("Delete result = %s", proto.ResultName(code))
	}
}
