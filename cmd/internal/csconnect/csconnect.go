// Package csconnect is the shared transport/URI plumbing for the ClassicStack file
// clients. It resolves the leading global flags (-ifacetype/-iface/-fork/-mac/-transport/
// -v), builds a client/link.Opener validated against a URI scheme's declared transports,
// and opens a target as an fs.ForkFS via the client SDK. Both cmd/csfs (the CLI) and
// cmd/csmount (the WinFsp mount) drive it, so the scheme×ifacetype matrix and the fork-
// backend selection have a single source of truth.
package csconnect

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/client"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// Config holds the resolved global flags common to the file clients.
type Config struct {
	IfaceType string // ltoudp | tashtalk | pcap | tcp
	Iface     string // interface/device/host
	Fork      string // fork container: appledouble | applesingle | derez | passthrough | native | nofork
	MAC       string // virtual-station MAC for raw-Ethernet transports (empty = random)
	Transport string // pcap sub-carrier for SMB: ipx (default) | nbipx | nbf
	Verbose   bool   // -v: print client wire-trace (NBP/ATP/ASP) to stderr
}

// ParseGlobalFlags peels the leading -flag/value pairs off args (a hand-rolled parser
// so flags may precede the subcommand without the stdlib flag package swallowing the
// command). It stops at the first non-flag token and returns the rest.
func ParseGlobalFlags(args []string) (Config, []string, error) {
	cfg := Config{}
	i := 0
	for i < len(args) {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			break
		}
		name := strings.TrimLeft(a, "-")
		// A boolean flag (-v / -verbose) takes no value; handle it before consuming the
		// next token so it may sit anywhere among the flags.
		if base, _, _ := strings.Cut(name, "="); base == "v" || base == "verbose" {
			cfg.Verbose = true
			i++
			continue
		}
		// Support -flag=value and -flag value for value-taking flags.
		var val string
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			val = name[eq+1:]
			name = name[:eq]
		} else if i+1 < len(args) {
			val = args[i+1]
			i++
		}
		switch name {
		case "ifacetype":
			cfg.IfaceType = strings.ToLower(val)
		case "iface":
			cfg.Iface = val
		case "fork":
			cfg.Fork = val
		case "mac":
			cfg.MAC = val
		case "transport":
			cfg.Transport = strings.ToLower(val)
		default:
			return cfg, nil, fmt.Errorf("unknown flag -%s", name)
		}
		i++
	}
	return cfg, args[i:], nil
}

// OpenerFor builds a client/link.Opener for a target, resolving and VALIDATING the
// transport against the scheme's declared transports. An -ifacetype that the scheme
// does not accept is rejected with a clear message (e.g. ltoudp is AFP-over-DDP only);
// an omitted -ifacetype takes the scheme's default.
func OpenerFor(cfg Config, target uri.Target) (*clientlink.Opener, error) {
	transports := client.TransportsFor(target.Scheme)

	kind := cfg.IfaceType
	if kind == "" {
		// URI-embedded transport (afp://…,ltoudp/…) overrides the default when present
		// and is a link kind; otherwise the scheme default.
		if target.Transport != "" && isLinkKind(target.Transport) {
			kind = target.Transport
		} else {
			kind = transports.Default
		}
	}
	if kind == "" {
		return nil, fmt.Errorf("%s: no default transport; pass -ifacetype (accepted: %s)",
			target.Scheme, strings.Join(transports.Kinds, ", "))
	}
	if !transports.Accepts(kind) {
		return nil, fmt.Errorf("-ifacetype %q is not valid for %s; accepted: %s",
			kind, target.Scheme, strings.Join(transports.Kinds, ", "))
	}

	// Resolve the pcap sub-carrier (Spec.Carrier). The explicit -transport flag wins;
	// otherwise a URI ",<transport>" tail that is NOT a link kind (e.g. smb://host,nbf/)
	// names the carrier — the URI grammar's documented way to pick ipx|nbipx|nbf without
	// the flag. (A link-kind tail like ",tcp" already selected `kind` above.)
	carrier := cfg.Transport
	if carrier == "" && target.Transport != "" && !isLinkKind(target.Transport) {
		carrier = target.Transport
	}

	spec := clientlink.Spec{Kind: kind, Name: cfg.Iface, Carrier: carrier}
	opener := clientlink.NewOpener(spec)
	if cfg.MAC != "" {
		mac, err := ParseMAC(cfg.MAC)
		if err != nil {
			return nil, err
		}
		opener.MAC = mac
	}
	return opener, nil
}

// ParseMAC parses a colon-, dash-, or bare-hex MAC address into a 6-byte array for the
// virtual-station source node of a raw-Ethernet transport (SMB-over-IPX). An empty -mac
// flag never reaches here (the transport then synthesises a random one).
func ParseMAC(s string) ([6]byte, error) {
	hw, err := net.ParseMAC(s)
	if err != nil || len(hw) != 6 {
		return [6]byte{}, fmt.Errorf("invalid -mac %q: want a 6-byte MAC (aa:bb:cc:dd:ee:ff)", s)
	}
	var mac [6]byte
	copy(mac[:], hw)
	return mac, nil
}

// isLinkKind reports whether s names a client/link transport kind (so a URI-embedded
// ",<transport>" that is a link kind can select it, vs. a protocol-native transport
// tag the factory interprets).
func isLinkKind(s string) bool {
	switch strings.ToLower(s) {
	case clientlink.KindLToUDP, clientlink.KindTashTalk, clientlink.KindPcap, clientlink.KindTCP, clientlink.KindInmem:
		return true
	}
	return false
}

// Connect parses a URI and opens it as an fs.ForkFS via the client SDK, applying the
// -fork container override. It is the shared open path for both the CLI and the mount.
func Connect(ctx context.Context, cfg Config, rawURI string) (fs.ForkFS, uri.Target, error) {
	target, err := uri.Parse(rawURI)
	if err != nil {
		return nil, uri.Target{}, err
	}
	opener, err := OpenerFor(cfg, target)
	if err != nil {
		return nil, target, err
	}
	remote, err := client.Connect(ctx, target, client.Options{
		Opener:      opener,
		ForkBackend: cfg.Fork,
	})
	if err != nil {
		return nil, target, err
	}
	return remote, target, nil
}
