package finder

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/client/xfer"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

func (s *Service) sessionFS(id string) (*Session, fs.ForkFS, error) {
	sess, err := s.get(id)
	if err != nil {
		return nil, nil, err
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return nil, nil, err
	}
	return sess, ffs, nil
}

func (s *Service) destPath(sess *Session, parent NodeRef, name string) (string, error) {
	parentPath, err := s.storePath(sess, parent)
	if err != nil {
		return "", err
	}
	name = strings.Trim(name, "/")
	if name == "" || strings.Contains(name, "/") {
		return "", fmt.Errorf("finder: invalid dest name %q", name)
	}
	return joinStore(parentPath, name), nil
}

func (s *Service) removeIfReplace(sess *Session, parent NodeRef, name string, replace bool) error {
	if !replace {
		return nil
	}
	if sess.readOnly {
		return ErrReadOnly
	}
	ffs, err := sess.requireFS()
	if err != nil {
		return err
	}
	dir, err := s.storePath(sess, parent)
	if err != nil {
		return err
	}
	path := joinStore(dir, name)
	info, err := ffs.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return xfer.Remove(ffs, path)
	}
	return ffs.Remove(path)
}

// Copy streams src to destParent/destName across two open Finder sessions.
func (s *Service) Copy(ctx context.Context, req TransferRequest, emit func(OpProgress)) error {
	srcSess, srcFS, err := s.sessionFS(req.SrcSession)
	if err != nil {
		return err
	}
	dstSess, dstFS, err := s.sessionFS(req.DestSession)
	if err != nil {
		return err
	}
	if dstSess.readOnly {
		return ErrReadOnly
	}
	srcPath, err := s.storePath(srcSess, req.SrcID)
	if err != nil {
		return err
	}
	dstPath, err := s.destPath(dstSess, req.DestParent, req.DestName)
	if err != nil {
		return err
	}
	if err := s.removeIfReplace(dstSess, req.DestParent, req.DestName, req.Replace); err != nil {
		return err
	}
	s.log.Log2(log.Debug, "finder copy", log.Str("src", srcPath), log.Str("dst", dstPath))
	progress := func(p xfer.Progress) {
		if emit == nil {
			return
		}
		emit(OpProgress{
			Phase:      PhaseCopying,
			Path:       p.Path,
			BytesDone:  p.BytesDone,
			BytesTotal: p.BytesTotal,
			DestName:   req.DestName,
		})
	}
	if err := xfer.CopyCtx(ctx, srcFS, dstFS, srcPath, dstPath, progress); err != nil {
		return err
	}
	if emit != nil {
		emit(OpProgress{Phase: PhaseCopying, DestName: req.DestName, Done: true})
	}
	return nil
}

// MoveAcross copies then deletes src across two open Finder sessions.
func (s *Service) MoveAcross(ctx context.Context, req TransferRequest, emit func(OpProgress)) error {
	if req.SrcSession == req.DestSession {
		return s.moveWithinSession(ctx, req, emit)
	}
	srcSess, srcFS, err := s.sessionFS(req.SrcSession)
	if err != nil {
		return err
	}
	dstSess, dstFS, err := s.sessionFS(req.DestSession)
	if err != nil {
		return err
	}
	if dstSess.readOnly || srcSess.readOnly {
		return ErrReadOnly
	}
	srcPath, err := s.storePath(srcSess, req.SrcID)
	if err != nil {
		return err
	}
	dstPath, err := s.destPath(dstSess, req.DestParent, req.DestName)
	if err != nil {
		return err
	}
	if err := s.removeIfReplace(dstSess, req.DestParent, req.DestName, req.Replace); err != nil {
		return err
	}
	s.log.Log2(log.Debug, "finder move across", log.Str("src", srcPath), log.Str("dst", dstPath))
	progress := func(p xfer.Progress) {
		if emit == nil {
			return
		}
		emit(OpProgress{
			Phase:      PhaseMoving,
			Path:       p.Path,
			BytesDone:  p.BytesDone,
			BytesTotal: p.BytesTotal,
			DestName:   req.DestName,
		})
	}
	if err := xfer.MoveAcrossCtx(ctx, srcFS, dstFS, srcPath, dstPath, progress); err != nil {
		return err
	}
	if emit != nil {
		emit(OpProgress{Phase: PhaseMoving, DestName: req.DestName, Done: true})
	}
	return nil
}

func (s *Service) moveWithinSession(ctx context.Context, req TransferRequest, emit func(OpProgress)) error {
	sess, ffs, err := s.sessionFS(req.SrcSession)
	if err != nil {
		return err
	}
	if sess.readOnly {
		return ErrReadOnly
	}
	srcPath, err := s.storePath(sess, req.SrcID)
	if err != nil {
		return err
	}
	dstPath, err := s.destPath(sess, req.DestParent, req.DestName)
	if err != nil {
		return err
	}
	if err := s.removeIfReplace(sess, req.DestParent, req.DestName, req.Replace); err != nil {
		return err
	}
	_ = ctx
	s.log.Log2(log.Debug, "finder move", log.Str("src", srcPath), log.Str("dst", dstPath))
	if err := ffs.Rename(srcPath, dstPath); err != nil {
		return err
	}
	if emit != nil {
		emit(OpProgress{Phase: PhaseMoving, DestName: req.DestName, Done: true})
	}
	return nil
}
