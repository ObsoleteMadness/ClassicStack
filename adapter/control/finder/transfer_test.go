package finder

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

func putMemSession(t *testing.T, svc *Service, id, volume string) *Session {
	t.Helper()
	ffs, err := fs.BuildShare(fs.ShareSpec{Name: volume, FSType: "memfs"}, nil)
	if err != nil {
		t.Fatalf("BuildShare: %v", err)
	}
	sess := &Session{
		ID:      id,
		Kind:    "local",
		Volume:  volume,
		FS:      ffs,
		local:   true,
		touched: time.Now(),
	}
	svc.put(sess)
	return sess
}

func TestCopyAcrossSessions(t *testing.T) {
	svc := New(nil, nil)
	src := putMemSession(t, svc, "src", "A")
	dst := putMemSession(t, svc, "dst", "B")
	root := src.FS.Meta().EnsureCNID("")
	dstRoot := dst.FS.Meta().EnsureCNID("")

	dir, err := svc.Mkdir("src", CNIDRef(root), "Folder")
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	file, err := svc.CreateFile("src", CNIDRef(dir.ID), "hello.txt", []byte("payload"), nil, nil)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}

	var last OpProgress
	err = svc.Copy(context.Background(), TransferRequest{
		SrcSession:  "src",
		DestSession: "dst",
		SrcID:       CNIDRef(file.ID),
		DestParent:  CNIDRef(dstRoot),
		DestName:    "hello.txt",
	}, func(p OpProgress) { last = p })
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if !last.Done {
		t.Fatalf("progress not done: %+v", last)
	}

	got, err := svc.Lookup("dst", CNIDRef(dstRoot), "hello.txt")
	if err != nil {
		t.Fatalf("Lookup dest: %v", err)
	}
	data, err := svc.ReadFork("dst", CNIDRef(got.ID), false, 0, 0)
	if err != nil {
		t.Fatalf("ReadFork: %v", err)
	}
	if !bytes.Equal(data, []byte("payload")) {
		t.Fatalf("data %q", data)
	}
	if _, err := svc.GetNode("src", CNIDRef(file.ID)); err != nil {
		t.Fatalf("source should remain: %v", err)
	}
}

func TestMoveWithinSession(t *testing.T) {
	svc := New(nil, nil)
	sess := putMemSession(t, svc, "t", "Mem")
	root := sess.FS.Meta().EnsureCNID("")
	dir, err := svc.Mkdir("t", CNIDRef(root), "Folder")
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	file, err := svc.CreateFile("t", CNIDRef(root), "a.txt", []byte("x"), nil, nil)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	err = svc.MoveAcross(context.Background(), TransferRequest{
		SrcSession:  "t",
		DestSession: "t",
		SrcID:       CNIDRef(file.ID),
		DestParent:  CNIDRef(dir.ID),
		DestName:    "a.txt",
	}, nil)
	if err != nil {
		t.Fatalf("MoveAcross: %v", err)
	}
	if _, err := svc.Lookup("t", CNIDRef(dir.ID), "a.txt"); err != nil {
		t.Fatalf("lookup in folder: %v", err)
	}
}
