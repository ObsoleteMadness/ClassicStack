package main

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/client"
	clientafp "github.com/ObsoleteMadness/ClassicStack/client/afp"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
)

// browse.go implements the server-root listing: `csfs ls afp://server/` (no volume)
// prints the server's info and the volumes it advertises, each with the full AFP URI to
// mount it, instead of failing an FPOpenVol with an empty volume name. It is AFP-only;
// the other schemes address a share/volume/drive directly in the URI.

// maybeBrowseServer handles `ls` of a server root. It returns (true, exitcode) when arg
// is an AFP URI naming a server but no volume — having printed the server info + volume
// list — and (false, 0) otherwise, so the caller falls through to the normal path.
func maybeBrowseServer(cfg config, arg string) (bool, int) {
	if !looksLikeTarget(arg) {
		return false, 0
	}
	target, err := uri.Parse(arg)
	if err != nil {
		return false, 0 // let the normal path report the parse error
	}
	// Only AFP supports a server-root browse, and only when no volume/path was given.
	if target.Scheme != "afp" || target.Volume != "" || target.Path != "" {
		return false, 0
	}

	opener, err := openerFor(cfg, target)
	if err != nil {
		return true, fail(err)
	}
	listing, err := clientafp.Browse(target, client.Options{Opener: opener, ForkBackend: cfg.fork})
	if err != nil {
		return true, fail(err)
	}
	printServerListing(target, listing)
	return true, 0
}

// printServerListing prints the server info header and one line per volume with the
// full AFP URI to mount it (server field taken verbatim from the input URI so it can be
// pasted back). Volume names with a space are shown quoted for copy-paste convenience.
func printServerListing(target uri.Target, l clientafp.ServerListing) {
	name := l.ServerName
	if name == "" {
		name = target.Server
	}
	fmt.Printf("Server:  %s\n", name)
	if l.MachineType != "" {
		fmt.Printf("Machine: %s\n", l.MachineType)
	}
	if len(l.AFPVersions) > 0 {
		fmt.Printf("AFP:     %s\n", joinComma(l.AFPVersions))
	}
	if len(l.UAMs) > 0 {
		fmt.Printf("UAMs:    %s\n", joinComma(l.UAMs))
	}

	if len(l.Volumes) == 0 {
		fmt.Println("\nNo volumes available to this login.")
		return
	}
	fmt.Printf("\nVolumes (%d):\n", len(l.Volumes))
	for _, v := range l.Volumes {
		note := ""
		if v.HasPassword {
			note = "  (password required)"
		}
		fmt.Printf("  %-28s %s%s\n", v.Name, afpVolumeURI(target, v.Name), note)
	}
}

// afpVolumeURI builds the full afp:// URI to mount a volume: the input URI's credentials
// and server, with the volume as the path. A volume name containing a space or slash is
// percent-friendly here only for display — the shell user quotes it — so it is emitted
// verbatim after the server.
func afpVolumeURI(target uri.Target, volume string) string {
	cred := ""
	switch {
	case target.User != "" && target.Pass != "":
		cred = target.User + ":" + target.Pass + "@"
	case target.User != "":
		cred = target.User + ":@"
	}
	return fmt.Sprintf("afp://%s%s/%s", cred, target.Server, volume)
}

// joinComma joins strings with ", " (a tiny local helper to avoid pulling strings just
// for one call site).
func joinComma(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
