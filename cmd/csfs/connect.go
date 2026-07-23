package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/client"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// config holds the resolved global flags.
type config struct {
	ifaceType string // ltoudp | tashtalk | pcap | tcp
	iface     string // interface/device/host
	fork      string // host fork container
	mac       string // virtual-station MAC for raw-Ethernet transports (empty = random)
	verbose   bool   // -v: print client wire-trace (NBP/ATP/ASP) to stderr
}

// parseGlobalFlags peels the leading -flag/value pairs off args (a hand-rolled parser
// so flags may precede the subcommand without the stdlib flag package swallowing the
// command). It stops at the first non-flag token and returns the rest.
func parseGlobalFlags(args []string) (config, []string, error) {
	cfg := config{}
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
			cfg.verbose = true
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
			cfg.ifaceType = strings.ToLower(val)
		case "iface":
			cfg.iface = val
		case "fork":
			cfg.fork = val
		case "mac":
			cfg.mac = val
		default:
			return cfg, nil, fmt.Errorf("unknown flag -%s", name)
		}
		i++
	}
	return cfg, args[i:], nil
}

// openerFor builds a client/link.Opener for a target, resolving and VALIDATING the
// transport against the scheme's declared transports. An -ifacetype that the scheme
// does not accept is rejected with a clear message (e.g. ltoudp is AFP-over-DDP only);
// an omitted -ifacetype takes the scheme's default.
func openerFor(cfg config, target uri.Target) (*clientlink.Opener, error) {
	transports := client.TransportsFor(target.Scheme)

	kind := cfg.ifaceType
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

	spec := clientlink.Spec{Kind: kind, Name: cfg.iface}
	opener := clientlink.NewOpener(spec)
	if cfg.mac != "" {
		mac, err := parseMAC(cfg.mac)
		if err != nil {
			return nil, err
		}
		opener.MAC = mac
	}
	return opener, nil
}

// parseMAC parses a colon-, dash-, or bare-hex MAC address into a 6-byte array for the
// virtual-station source node of a raw-Ethernet transport (SMB-over-IPX). An empty -mac
// flag never reaches here (the transport then synthesises a random one).
func parseMAC(s string) ([6]byte, error) {
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

// connect parses a URI and opens it as an fs.ForkFS via the client SDK, applying the
// -fork host container override.
func connect(cfg config, rawURI string) (fs.ForkFS, uri.Target, error) {
	target, err := uri.Parse(rawURI)
	if err != nil {
		return nil, uri.Target{}, err
	}
	opener, err := openerFor(cfg, target)
	if err != nil {
		return nil, target, err
	}
	remote, err := client.Connect(contextForRun(), target, client.Options{
		Opener:      opener,
		ForkBackend: cfg.fork,
	})
	if err != nil {
		return nil, target, err
	}
	return remote, target, nil
}

// hostShare opens a host directory as an fs.ForkFS (a local_fs share), so a host path is
// an ordinary ForkFS endpoint for cp — the same code path as a remote. The -fork flag
// selects the container (default appledouble). It returns the share plus the '/'-
// relative path within it and its root, split from the given host path.
func hostShare(cfg config, hostPath string) (fs.ForkFS, error) {
	fork := cfg.fork
	if fork == "" {
		fork = "appledouble"
	}
	return fs.BuildShare(fs.ShareSpec{
		FSType:      "local_fs",
		Path:        hostPath,
		ForkBackend: fork,
	}, nil)
}
