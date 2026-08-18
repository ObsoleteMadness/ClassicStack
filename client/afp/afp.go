// Package afp is the AFP client's fs.FileSystem + native fs.ForkEngine adapter: it maps
// the core/fs operations onto AFP commands over an ASP session (client/asp) — Enumerate
// → ReadDir, GetFileDirParms → Stat, OpenFork/Read/Write → the File I/O, and
// Get/SetFileDirParms → Finder info (type/creator). Because it implements fs.ForkEngine
// natively, client.Connect defaults to the "passthrough" fork backend so OpenFork hits
// the wire. Selecting a sidecar layout (-fork derez / appledouble) keeps that native
// OpenFork and PROJECTS .rdump/.idump / ._name into the FileSystem namespace for a
// Windows mount — the inverse of the server-hosting case.
//
// Paths are '/'-separated UTF-8 store paths (Windows / csfs); they are transcoded to
// MacRoman on the wire (PathTypeLongNames) via core/encoding.
//
// Ring: CLIENT.
package afp

import (
	"errors"
	stdfs "io/fs"
	"strings"
	"sync"
	"time"

	aspclient "github.com/ObsoleteMadness/ClassicStack/client/asp"
	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	"github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/core/encoding"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/atp"
)

// afpLog narrates FP* calls when csfs/csmount -v is on (client/trace).
var afpLog = trace.Logger("afp")

// pathType is the AFP path-type this client uses. Long names (MacRoman, 31 bytes) is the
// classic-server baseline; UTF-8 is an AFP-3 refinement not needed for a 2.x server.
const pathType = proto.PathTypeLongNames

// FS is an AFP client bound to one open volume. It satisfies fs.FileSystem and
// fs.ForkEngine (the "passthrough" fork backend forwards to it).
type FS struct {
	sess  *aspclient.Session
	volID uint16
	name  string

	// onClose, if set, runs after the session is closed (the factory sets it to close
	// the owning DDP endpoint, so FS.Close tears the whole transport down).
	onClose func()

	// Reconnect state: when the ASP session dies (server CloseSession / idle timeout)
	// we OpenSession + Login + OpenVol again on the same endpoint so a long-lived
	// mount (csmount) survives. Intentionally empty until connect() fills them.
	ep      *atalk.Endpoint
	sls     atalk.Addr
	user    string
	pass    string
	srvInfo proto.ServerInfo

	// onMessage delivers login/attention text to the Connect caller (Finder).
	onMessage func(kind, from, text string)

	mu       sync.Mutex
	readOnly bool
	closed   bool // intentional FS.Close — do not reconnect
	cache    attrCache
}

// Open logs into the server over sess and opens the named volume, returning the FS. The
// caller has already run FPLogin via Login; Open runs FPOpenVol.
func Open(sess *aspclient.Session, volume string) (*FS, error) {
	req := proto.OpenVolRequest{
		Bitmap:  proto.VolBitmapID | proto.VolBitmapSignature | proto.VolBitmapAttributes,
		VolName: volume,
	}
	body, result, err := sess.Command(req.Marshal())
	if err != nil {
		return nil, err
	}
	if result != proto.NoErr {
		return nil, afpError("FPOpenVol", result)
	}
	vp, ok := proto.ParseVolParams(body)
	if !ok {
		return nil, errMalformed("FPOpenVol reply")
	}
	return &FS{sess: sess, volID: vp.VolID, name: volume}, nil
}

// afpWirePath translates a '/'-separated, volume-root-relative UTF-8 store path to the
// AFP wire pathname: a leading NUL, then the elements joined by NUL, each encoded in
// the path-type charset (MacRoman for PathTypeLongNames). An empty path names the
// volume root and is sent as a single NUL (the "this directory" form the server accepts).
//
// Store paths are UTF-8 (as produced by afpDecodeName / Windows). Casting UTF-8 bytes
// straight onto the wire mangled non-ASCII MacRoman names (e.g. ™ U+2122 → three bytes
// instead of MacRoman 0xAA), so opens of those folders failed after a listing showed �.
func afpWirePath(p string) []byte {
	p = strings.Trim(p, "/")
	if p == "" {
		return []byte{0x00}
	}
	elems := strings.Split(p, "/")
	out := []byte{0x00}
	for i, e := range elems {
		if i > 0 {
			out = append(out, 0x00)
		}
		out = append(out, afpEncodeName(e)...)
	}
	return out
}

// afpEncodeName encodes one UTF-8 path element to MacRoman wire bytes. Unmappable
// runes are replaced with '?' so a partial name still reaches the server rather than
// dropping the whole request.
func afpEncodeName(utf8 string) []byte {
	b, err := encoding.UTF8ToMacRoman(utf8)
	if err == nil {
		return b
	}
	// Replace unmappable runes one-by-one so the rest of the name survives.
	out := make([]byte, 0, len(utf8))
	for _, r := range utf8 {
		if c, ok := encoding.RuneToMacRoman(r); ok {
			out = append(out, c)
		} else {
			out = append(out, '?')
		}
	}
	return out
}

// afpDecodeName decodes MacRoman wire name bytes to a UTF-8 store/Windows name.
func afpDecodeName(wire []byte) string {
	return encoding.MacRomanToUTF8(wire)
}

// splitPath splits a '/'-separated path into its parent and final element.
func splitPath(p string) (dir, base string) {
	p = strings.Trim(p, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i], p[i+1:]
	}
	return "", p
}

// childPath joins a directory store path and a child name.
func childPath(dir, name string) string {
	dir = strings.Trim(dir, "/")
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// command runs an AFP command block and returns the reply body, mapping a non-zero
// result to an error. When -v is on it narrates the FP* name, path, and result.
// build is called with the current volume ID so a reconnect (new OpenVol) can rebuild
// the request rather than replaying a stale VolID.
func (f *FS) command(name, path string, build func(volID uint16) []byte) ([]byte, error) {
	body, result, err := f.sessCommand(name, path, build)
	if err != nil {
		return nil, err
	}
	if result != proto.NoErr {
		return nil, afpError(name, result)
	}
	return body, nil
}

// sessCommand runs an AFP command and narrates it under -v. Callers that need the raw
// AFP result code (Enumerate paging, OpenFork not-found) use this instead of command().
// path is included in the trace to make it easy to correlate wire calls with store paths.
//
// If the ASP session has been closed (server CloseSession / idle timeout), it
// re-establishes the session and retries the command once with a freshly built block.
// Fork-ref commands (FPRead/FPWrite/…) must use sessForkCommand instead: after a
// reconnect the old fork ref is dead and the caller has to OpenFork again.
func (f *FS) sessCommand(name, path string, build func(volID uint16) []byte) (body []byte, result int32, err error) {
	return f.sessCommandRetry(name, path, 1, build, true)
}

// sessCommandQuantum is sessCommand for replies that may fill an ASP quantum
// (FPEnumerate). The ATP bitmap asks for 8 slots, matching classicstack-web.
func (f *FS) sessCommandQuantum(name, path string, build func(volID uint16) []byte) (body []byte, result int32, err error) {
	return f.sessCommandRetry(name, path, atp.MaxResponsePackets, build, true)
}

// sessForkCommand is like sessCommand but does not retry after reconnect — the
// command's fork ref is invalid on the new session. It still re-establishes so the
// next OpenFork / path-based call succeeds, and returns ErrSessionClosed so the
// caller can reopen the fork and retry. maxResp is the ATP slot budget for the reply
// (FPRead sizes it to the requested byte count).
func (f *FS) sessForkCommand(name, path string, maxResp int, build func(volID uint16) []byte, extra ...log.Field) (body []byte, result int32, err error) {
	return f.sessCommandRetry(name, path, maxResp, build, false, extra...)
}

func (f *FS) sessCommandRetry(name, path string, maxResp int, build func(volID uint16) []byte, retry bool, extra ...log.Field) (body []byte, result int32, err error) {
	body, result, dead, err := f.sessCommandOnce(name, path, maxResp, build, extra...)
	if !errors.Is(err, aspclient.ErrSessionClosed) {
		return body, result, err
	}
	if rerr := f.reestablish(dead); rerr != nil {
		afpLog.Log2(log.Debug, "reconnect failed", log.Str("op", name), log.Str("err", rerr.Error()))
		return nil, 0, err
	}
	if !retry {
		afpLog.Log1(log.Debug, "reconnected; fork ref stale", log.Str("op", name))
		return nil, 0, aspclient.ErrSessionClosed
	}
	afpLog.Log1(log.Debug, "reconnected; retrying", log.Str("op", name))
	body, result, _, err = f.sessCommandOnce(name, path, maxResp, build, extra...)
	return body, result, err
}

func (f *FS) sessCommandOnce(name, path string, maxResp int, build func(volID uint16) []byte, extra ...log.Field) (body []byte, result int32, sess *aspclient.Session, err error) {
	start := time.Now()
	var volID uint16
	sess, volID = f.session()
	if sess == nil {
		logAFPCommand(name, path, maxResp, 0, 0, 0, aspclient.ErrSessionClosed, extra...)
		return nil, 0, nil, aspclient.ErrSessionClosed
	}
	body, result, err = sess.CommandMax(build(volID), maxResp)
	logAFPCommand(name, path, maxResp, len(body), result, time.Since(start).Milliseconds(), err, extra...)
	return body, result, sess, err
}

// logAFPCommand records one FP* round-trip. The sink threshold decides whether
// the line is printed; callers always emit.
func logAFPCommand(name, path string, maxResp, n int, result int32, ms int64, err error, extra ...log.Field) {
	fields := []log.Field{
		log.Str("op", name),
		log.Int("maxResp", int64(maxResp)),
		log.Int("n", int64(n)),
		log.Int("ms", ms),
	}
	if path != "" {
		fields = append(fields, log.Str("path", path))
	}
	fields = append(fields, extra...)
	if result != proto.NoErr {
		fields = append(fields, log.Int("result", int64(result)))
	}
	if err != nil {
		fields = append(fields, log.Str("err", err.Error()))
	}
	afpLog.Log(log.Debug, "command", fields...)
}

// sessWrite runs an ASP Write (FPWrite) with the same session-closed reconnect as
// sessCommand. On ErrSessionClosed it re-establishes but does NOT auto-retry: the
// fork ref in the header is stale until the caller reopens the fork.
func (f *FS) sessWrite(path string, header []byte, data []byte) (body []byte, result int32, err error) {
	start := time.Now()
	sess, _ := f.session()
	if sess == nil {
		logAFPCommand("FPWrite", path, 1, 0, 0, 0, aspclient.ErrSessionClosed)
		return nil, 0, aspclient.ErrSessionClosed
	}
	body, result, err = sess.Write(header, data)
	logAFPCommand("FPWrite", path, 1, len(data), result, time.Since(start).Milliseconds(), err)
	if !errors.Is(err, aspclient.ErrSessionClosed) {
		return body, result, err
	}
	if rerr := f.reestablish(sess); rerr != nil {
		return nil, 0, err
	}
	return nil, 0, aspclient.ErrSessionClosed // caller must reopen fork and retry
}

// session returns the current ASP session and volume ID under the FS lock.
func (f *FS) session() (*aspclient.Session, uint16) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sess, f.volID
}

// reestablish opens a new ASP session on the existing endpoint, logs in, and re-opens
// the volume. dead is the session that returned ErrSessionClosed; if another goroutine
// already replaced it, this is a no-op. Intentional FS.Close sets closed and skips
// reconnect.
func (f *FS) reestablish(dead *aspclient.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return aspclient.ErrSessionClosed
	}
	if f.sess != dead {
		// Another caller already reconnected (or cleared sess on failure).
		if f.sess != nil {
			return nil
		}
		if dead != nil {
			// Prior reconnect failed and left sess nil; fall through to try again.
		}
	}
	if f.ep == nil || f.name == "" {
		return errors.New("afp: cannot reconnect: no dial state")
	}

	if dead != nil {
		_ = dead.Close() // unbind WSS; idempotent if already stopped
	} else if f.sess != nil {
		_ = f.sess.Close()
	}
	f.sess = nil

	a := atalk.NewATP(f.ep)
	sess, err := aspclient.Open(f.ep, a, f.sls)
	if err != nil {
		return err
	}
	if err := LoginNegotiated(sess, f.user, f.pass, f.srvInfo); err != nil {
		_ = sess.Close()
		return err
	}
	req := proto.OpenVolRequest{
		Bitmap:  proto.VolBitmapID | proto.VolBitmapSignature | proto.VolBitmapAttributes,
		VolName: f.name,
	}
	body, result, err := sess.Command(req.Marshal())
	if err != nil {
		_ = sess.Close()
		return err
	}
	if result != proto.NoErr {
		_ = sess.Close()
		return afpError("FPOpenVol", result)
	}
	vp, ok := proto.ParseVolParams(body)
	if !ok {
		_ = sess.Close()
		return errMalformed("FPOpenVol reply")
	}
	f.sess = sess
	f.volID = vp.VolID
	f.cache.invalidateAll()
	afpLog.Log1(log.Debug, "session re-established", log.Str("vol", f.name))
	// The new ASP session needs its own attention handler; the old WSS loop is gone.
	sess.SetAttentionHandler(f.handleAttention)
	return nil
}

// fileInfo is the fs.FileInfo the adapter returns from parsed AFP params.
type fileInfo struct {
	name       string
	size       int64
	rsrcLen    int64 // resource-fork length when the bitmap requested it
	dir        bool
	modTime    time.Time
	createTime time.Time
	afpAttrs   uint16 // FPGetFileDirParms Attributes word (AFP AttrInvisible/System/…)
	finder     [32]byte
	hasFinder  bool
}

func (fi fileInfo) Name() string { return fi.name }
func (fi fileInfo) Size() int64  { return fi.size }
func (fi fileInfo) Mode() stdfs.FileMode {
	if fi.dir {
		return stdfs.ModeDir | 0o755
	}
	return 0o644
}
func (fi fileInfo) ModTime() time.Time { return fi.modTime }
func (fi fileInfo) IsDir() bool        { return fi.dir }

// Sys exposes AFP-derived metadata to consumers above the FileSystem: DOS attributes
// (WinFsp MetaEngine), creation time, resource-fork length, and Finder info (the
// sidecar-export projector uses the last two to decide which .rdump / .idump / ._name
// entries to synthesise without extra round-trips).
func (fi fileInfo) Sys() any {
	return afpMeta{
		dos:       afpAttrsToDOS(fi.afpAttrs),
		create:    fi.createTime,
		rsrcLen:   fi.rsrcLen,
		finder:    fi.finder,
		hasFinder: fi.hasFinder,
	}
}

// afpMeta adapts AFP-derived metadata to the fs interfaces the MetaEngine and the
// sidecar-export projector read (DOSAttrInfo, DOSCreateTimeInfo, ResourceLenInfo,
// FinderInfoBits).
type afpMeta struct {
	dos       uint16
	create    time.Time
	rsrcLen   int64
	finder    [32]byte
	hasFinder bool
}

func (m afpMeta) DOSAttrs() uint16             { return m.dos }
func (m afpMeta) DOSCreateTime() time.Time     { return m.create }
func (m afpMeta) ResourceForkLen() int64       { return m.rsrcLen }
func (m afpMeta) FinderInfo() ([32]byte, bool) { return m.finder, m.hasFinder }

// afpAttrsToDOS maps the AFP file/dir Attributes word to the DOS attribute bits.
func afpAttrsToDOS(a uint16) uint16 {
	var d uint16
	if a&proto.AttrInvisible != 0 {
		d |= fs.DOSHidden
	}
	if a&proto.AttrSystem != 0 {
		d |= fs.DOSSystem
	}
	if a&proto.AttrWriteInhibit != 0 {
		d |= fs.DOSReadOnly
	}
	return d
}

// dirEntry is the fs.DirEntry the adapter returns from Enumerate.
type dirEntry struct {
	name      string
	dir       bool
	size      int64
	rsrcLen   int64
	mod       time.Time
	create    time.Time
	afpAttrs  uint16
	finder    [32]byte
	hasFinder bool
}

func (d dirEntry) Name() string { return d.name }
func (d dirEntry) IsDir() bool  { return d.dir }
func (d dirEntry) Type() stdfs.FileMode {
	if d.dir {
		return stdfs.ModeDir
	}
	return 0
}
func (d dirEntry) Info() (stdfs.FileInfo, error) {
	return fileInfo{
		name: d.name, size: d.size, rsrcLen: d.rsrcLen, dir: d.dir,
		modTime: d.mod, createTime: d.create, afpAttrs: d.afpAttrs,
		finder: d.finder, hasFinder: d.hasFinder,
	}, nil
}
