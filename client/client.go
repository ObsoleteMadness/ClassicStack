// Package client is the ClassicStack file-client SDK: the client-side mirror of
// core/fs's BuildShare. It lets a caller address a legacy file server with a URI and
// obtain an fs.ForkFS it can drive with the ordinary core/fs operations — the same
// interface the SERVERS implement, so a remote AFP/SMB/NCP/EtherDFS volume is
// indistinguishable from a local one to everything above it (client/xfer, cmd/csfs).
//
// The design deliberately parallels core/fs:
//
//	core/fs                     client
//	-------                     ------
//	RegisterFS(fsType, …)       RegisterClient(scheme, …)
//	Factory(spec, bus, store)   Factory(ctx, target, opts)
//	BuildShare(spec, bus)       Connect(ctx, target, opts)
//	  → f(spec,…) then WrapBase   → f(ctx,…) then fs.WrapBase
//	  returns fs.ForkFS           returns fs.ForkFS
//
// So Connect resolves the scheme's factory, builds the protocol base FileSystem, then
// layers the SAME core/fs fork and meta engines fs.WrapBase layers over a local
// backend. Protocols with a native fork concept (AFP) return a base that itself
// implements fs.ForkEngine and select the "passthrough" fork backend; the others
// (SMB/NCP/EtherDFS) take the default AppleDouble adapter, which reads/writes the
// server's own "._name" sidecars as ordinary files on the wire.
//
// Ring: CLIENT (top-level; may import adapter/ and core/, unlike core/).
package client

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// Options carries the resolved, protocol-agnostic knobs a Connect needs beyond the
// URI: how to open the transport, and how to assemble the fork/meta stack.
type Options struct {
	// Opener builds the transport link a factory asks for (pcap/ltoudp/tashtalk/tcp/
	// inmem). It is REQUIRED — a factory reaches its wire only through it, so tests
	// can substitute an in-memory opener. See client/link.
	Opener *link.Opener
	// ForkBackend overrides the host-container / remote fork adapter. Empty lets the
	// scheme pick its default (AFP: "passthrough" native forks; others: "appledouble").
	// A CLI -fork flag threads through here.
	ForkBackend string
	// MetaBackend / FilenameCodec mirror fs.ShareSpec; empty takes core/fs defaults.
	MetaBackend   string
	FilenameCodec string
	// ReadOnly opens the remote volume read-only when the protocol supports it.
	ReadOnly bool
}

// Factory builds a protocol client's base fs.FileSystem for one connection. It parses
// target.Server (its protocol-native address form), opens the transport through
// opts.Opener, logs in with target.User/target.Pass, opens target.Volume, and returns
// a base FileSystem rooted at that volume. A factory whose protocol has native forks
// returns a FileSystem that also implements fs.ForkEngine (reached via the
// "passthrough" fork backend). The returned Base may implement fs.FSCloser to release
// the session at Close; Connect's ForkFS forwards Close through to it.
type Factory func(ctx context.Context, target uri.Target, opts Options) (fs.FileSystem, error)

// registeredClient is one scheme's factory plus its declared default fork backend and
// param schema, mirroring core/fs.registeredFS.
type registeredClient struct {
	factory     Factory
	defaultFork string
	params      []fs.Param
}

var (
	clientMu   sync.RWMutex
	clientRegs = map[string]registeredClient{}
)

// RegisterClient registers a scheme's client factory, mirroring fs.RegisterFS. name is
// the URI scheme ("afp", "smb", "ncp", "etherdfs"). defaultFork is the fork backend
// Connect uses when Options.ForkBackend is empty ("passthrough" for a native-fork
// protocol, "appledouble" otherwise). params declares the credentials/volume keys a UI
// could render (like fs.Param); it may be nil.
func RegisterClient(name, defaultFork string, f Factory, params ...fs.Param) {
	clientMu.Lock()
	defer clientMu.Unlock()
	clientRegs[strings.ToLower(name)] = registeredClient{factory: f, defaultFork: defaultFork, params: params}
}

// Schemes returns the registered scheme names, sorted, mirroring fs.Types.
func Schemes() []string {
	clientMu.RLock()
	out := make([]string, 0, len(clientRegs))
	for s := range clientRegs {
		out = append(out, s)
	}
	clientMu.RUnlock()
	sort.Strings(out)
	return out
}

// ParamsFor returns a scheme's declared param schema, mirroring fs.ParamsFor.
func ParamsFor(scheme string) []fs.Param {
	clientMu.RLock()
	defer clientMu.RUnlock()
	return clientRegs[strings.ToLower(scheme)].params
}

// ErrUnknownScheme is returned by Connect when no factory is registered for the URI's
// scheme (typically because its build tag / package was not linked in).
var ErrUnknownScheme = errors.New("client: unknown scheme")

func lookupClient(scheme string) (registeredClient, bool) {
	clientMu.RLock()
	defer clientMu.RUnlock()
	r, ok := clientRegs[strings.ToLower(scheme)]
	return r, ok
}

// Connect resolves target.Scheme to its factory, builds the protocol base FileSystem,
// and layers the mandatory core/fs fork + meta engines over it via fs.WrapBase —
// returning an fs.ForkFS the caller drives like any local share. It is the client-side
// fs.BuildShare. The returned ForkFS's Close (fs.FSCloser) tears the session down.
func Connect(ctx context.Context, target uri.Target, opts Options) (fs.ForkFS, error) {
	reg, ok := lookupClient(target.Scheme)
	if !ok {
		return nil, ErrUnknownScheme
	}
	if opts.Opener == nil {
		return nil, errors.New("client: Options.Opener is required")
	}

	base, err := reg.factory(ctx, target, opts)
	if err != nil {
		return nil, err
	}

	fork := opts.ForkBackend
	if fork == "" {
		fork = reg.defaultFork
	}

	// The client keeps its CNID/derived-name state in-memory: a client session is
	// transient, so there is no on-disk metastore to snapshot. Names/attrs the remote
	// volume already carries come over the wire; this store only backs the local
	// AppleDouble adapter's bookkeeping for the duration of the connection.
	store, err := metastore.Open("mem", "")
	if err != nil {
		return nil, err
	}

	spec := fs.ShareSpec{
		Name:          target.Volume,
		ForkBackend:   fork,
		MetaBackend:   opts.MetaBackend,
		FilenameCodec: opts.FilenameCodec,
		ReadOnly:      opts.ReadOnly,
	}
	return fs.WrapBase(base, spec, store)
}
