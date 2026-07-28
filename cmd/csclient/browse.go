package main

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/client"
	clientafp "github.com/ObsoleteMadness/ClassicStack/client/afp"
	clientncp "github.com/ObsoleteMadness/ClassicStack/client/ncp"
	clientsmb "github.com/ObsoleteMadness/ClassicStack/client/smb"
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
	// A server-root browse applies only when no volume/path was given. AFP lists volumes;
	// SMB lists shares (RAP NetShareEnum over IPC$). Other schemes address a share/drive
	// directly in the URI.
	if target.Volume != "" || target.Path != "" {
		return false, 0
	}

	opener, err := openerFor(cfg, target)
	if err != nil {
		return true, fail(err)
	}
	opts := client.Options{Opener: opener, ForkBackend: cfg.Fork}

	switch target.Scheme {
	case "afp":
		listing, err := clientafp.Browse(target, opts)
		if err != nil {
			return true, fail(err)
		}
		printServerListing(target, listing)
		return true, 0
	case "smb":
		listing, err := clientsmb.Browse(target, opts)
		if err != nil {
			return true, fail(err)
		}
		printSMBListing(target, listing)
		return true, 0
	case "ncp":
		listing, err := clientncp.Browse(target, opts)
		if err != nil {
			return true, fail(err)
		}
		printNCPListing(target, listing)
		return true, 0
	}
	return false, 0
}

// printNCPListing prints the server label and one line per mounted volume, each with the
// full ncp:// URI to mount it (the input URI's server verbatim, the volume as the path).
func printNCPListing(target uri.Target, l clientncp.ServerListing) {
	name := l.ServerName
	if name == "" {
		name = target.Server
	}
	fmt.Printf("Server:  %s\n", name)

	if len(l.Volumes) == 0 {
		fmt.Println("\nNo volumes available to this login.")
		return
	}
	fmt.Printf("\nVolumes (%d):\n", len(l.Volumes))
	for _, v := range l.Volumes {
		fmt.Printf("  %-28s %s\n", v, ncpVolumeURI(target, v))
	}
}

// ncpVolumeURI builds the full ncp:// URI to mount a volume: the input URI's credentials
// and server (with any ,transport tail), and the volume as the path.
func ncpVolumeURI(target uri.Target, volume string) string {
	cred := ""
	switch {
	case target.User != "" && target.Pass != "":
		cred = target.User + ":" + target.Pass + "@"
	case target.User != "":
		cred = target.User + ":@"
	}
	server := target.Server
	if target.Transport != "" {
		server += "," + target.Transport
	}
	return fmt.Sprintf("ncp://%s%s/%s", cred, server, volume)
}

// printSMBListing prints the server label and one line per share, with the full smb:// URI
// to connect to each (the input URI's server verbatim, the share as the path). The IPC$
// pipe is shown but marked, since it is not a mountable file share.
func printSMBListing(target uri.Target, l clientsmb.ServerListing) {
	name := l.ServerName
	if name == "" {
		name = target.Server
	}
	fmt.Printf("Server:  %s\n", name)
	if l.Dialect != "" {
		fmt.Printf("Dialect: %s\n", l.Dialect)
	}

	if len(l.Shares) == 0 {
		fmt.Println("\nNo shares available to this login.")
		return
	}
	fmt.Printf("\nShares (%d):\n", len(l.Shares))
	for _, sh := range l.Shares {
		kind := "disk"
		uriStr := smbShareURI(target, sh.Name)
		if sh.IsIPC {
			kind = "IPC$"
			uriStr = ""
		}
		remark := sh.Comment
		if remark != "" {
			remark = "  — " + remark
		}
		fmt.Printf("  %-16s %-6s %s%s\n", sh.Name, kind, uriStr, remark)
	}
}

// smbShareURI builds the full smb:// URI to connect to a share: the input URI's
// credentials, server (with any ,transport tail), and the share as the path.
func smbShareURI(target uri.Target, share string) string {
	cred := ""
	switch {
	case target.User != "" && target.Pass != "":
		cred = target.User + ":" + target.Pass + "@"
	case target.User != "":
		cred = target.User + ":@"
	}
	server := target.Server
	if target.Transport != "" {
		server += "," + target.Transport
	}
	return fmt.Sprintf("smb://%s%s/%s", cred, server, share)
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
