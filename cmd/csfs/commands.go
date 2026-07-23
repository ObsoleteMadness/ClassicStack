package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/client/xfer"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// endpoint is a resolved cp/ls target: a ForkFS, the '/'-relative path within it, and a
// closer to release it. A URI endpoint is a remote share opened at its volume root (the
// path is the URI's Path); a host endpoint is a local_fs share rooted at the file's
// parent directory (the path is its basename).
type endpoint struct {
	fsys  fs.ForkFS
	path  string
	close func()
	label string
}

// resolveEndpoint turns an argument (a URI or a host path) into an endpoint.
func resolveEndpoint(cfg config, arg string) (endpoint, error) {
	if looksLikeTarget(arg) {
		remote, target, err := connect(cfg, arg)
		if err != nil {
			return endpoint{}, err
		}
		return endpoint{
			fsys:  remote,
			path:  target.Path,
			close: func() { _ = fs.CloseFS(remote) },
			label: target.Redacted(),
		}, nil
	}
	// Host path: root a local_fs share at the parent dir, address the basename.
	abs, err := filepath.Abs(arg)
	if err != nil {
		return endpoint{}, err
	}
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)
	sh, err := hostShare(cfg, dir)
	if err != nil {
		return endpoint{}, err
	}
	return endpoint{
		fsys:  sh,
		path:  base,
		close: func() { _ = fs.CloseFS(sh) },
		label: arg,
	}, nil
}

// runOneShot dispatches a one-shot subcommand.
func runOneShot(cfg config, cmd string, args []string) int {
	switch cmd {
	case "ls":
		return cmdLs(cfg, args)
	case "cp", "get", "put":
		return cmdCp(cfg, args)
	case "mv":
		return cmdMv(cfg, args)
	case "rm":
		return cmdRm(cfg, args)
	case "attrib":
		return cmdAttrib(cfg, args)
	case "type":
		return cmdTypeCreator(cfg, args, true)
	case "creator":
		return cmdTypeCreator(cfg, args, false)
	}
	return 2
}

func cmdLs(cfg config, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: csfs ls <uri>")
		return 2
	}
	// A server-root AFP URI (afp://server/ with no volume) lists the server's volumes
	// and info instead of opening a volume (which would fail with an empty name).
	if done, code := maybeBrowseServer(cfg, args[0]); done {
		return code
	}
	ep, err := resolveEndpoint(cfg, args[0])
	if err != nil {
		return fail(err)
	}
	defer ep.close()
	return listDir(ep.fsys, ep.path)
}

// listDir prints a directory listing with type/creator and DOS attrs.
func listDir(sh fs.ForkFS, path string) int {
	entries, err := xfer.List(sh, path)
	if err != nil {
		return fail(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	for _, e := range entries {
		kind := "-"
		if e.IsDir {
			kind = "d"
		}
		tc := ""
		if e.Type != "" || e.Creator != "" {
			tc = fmt.Sprintf("  %-4s/%-4s", e.Type, e.Creator)
		}
		rsrc := ""
		if e.RsrcSize > 0 {
			rsrc = fmt.Sprintf("  rsrc=%d", e.RsrcSize)
		}
		fmt.Printf("%s %10d  %s%s%s\n", kind, e.Size, e.Name, tc, rsrc)
	}
	return 0
}

func cmdCp(cfg config, args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: csfs cp <src> <dst>")
		return 2
	}
	src, err := resolveEndpoint(cfg, args[0])
	if err != nil {
		return fail(err)
	}
	defer src.close()
	dst, err := resolveEndpoint(cfg, args[1])
	if err != nil {
		return fail(err)
	}
	defer dst.close()

	if err := xfer.Copy(src.fsys, dst.fsys, src.path, dst.path); err != nil {
		return fail(err)
	}
	fmt.Printf("copied %s -> %s\n", src.label, dst.label)
	return 0
}

func cmdMv(cfg config, args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: csfs mv <uri> <newpath>")
		return 2
	}
	ep, err := resolveEndpoint(cfg, args[0])
	if err != nil {
		return fail(err)
	}
	defer ep.close()
	newPath := strings.Trim(args[1], "/")
	if err := xfer.Move(ep.fsys, ep.path, newPath); err != nil {
		return fail(err)
	}
	fmt.Printf("moved %s -> %s\n", ep.path, newPath)
	return 0
}

func cmdRm(cfg config, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: csfs rm <uri>")
		return 2
	}
	ep, err := resolveEndpoint(cfg, args[0])
	if err != nil {
		return fail(err)
	}
	defer ep.close()
	if err := xfer.Remove(ep.fsys, ep.path); err != nil {
		return fail(err)
	}
	fmt.Printf("removed %s\n", ep.path)
	return 0
}

func cmdAttrib(cfg config, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: csfs attrib <uri> [+r|-r|+h|-h|+s|-s|+a|-a]")
		return 2
	}
	ep, err := resolveEndpoint(cfg, args[0])
	if err != nil {
		return fail(err)
	}
	defer ep.close()
	if len(args) == 1 {
		attr, _ := ep.fsys.Meta().Attrs(ep.path)
		fmt.Println(formatAttrs(attr.Attrs))
		return 0
	}
	var set, clear uint16
	for _, tok := range args[1:] {
		bit, ok := attrBit(tok)
		if !ok {
			fmt.Fprintf(os.Stderr, "csfs: bad attrib token %q\n", tok)
			return 2
		}
		if tok[0] == '+' {
			set |= bit
		} else {
			clear |= bit
		}
	}
	if err := xfer.SetAttr(ep.fsys, ep.path, set, clear); err != nil {
		return fail(err)
	}
	return 0
}

func cmdTypeCreator(cfg config, args []string, isType bool) int {
	name := "type"
	if !isType {
		name = "creator"
	}
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: csfs %s <uri> [CODE]\n", name)
		return 2
	}
	ep, err := resolveEndpoint(cfg, args[0])
	if err != nil {
		return fail(err)
	}
	defer ep.close()
	if len(args) == 1 {
		fi, ok, _ := ep.fsys.ReadFinderInfo(ep.path)
		if !ok {
			fmt.Println("(none)")
			return 0
		}
		if isType {
			fmt.Println(strings.TrimRight(string(fi[0:4]), "\x00 "))
		} else {
			fmt.Println(strings.TrimRight(string(fi[4:8]), "\x00 "))
		}
		return 0
	}
	code := args[1]
	if isType {
		err = xfer.SetType(ep.fsys, ep.path, code)
	} else {
		err = xfer.SetCreator(ep.fsys, ep.path, code)
	}
	if err != nil {
		return fail(err)
	}
	return 0
}

// attrBit maps a "+x"/"-x" token to its DOS attribute bit.
func attrBit(tok string) (uint16, bool) {
	if len(tok) != 2 || (tok[0] != '+' && tok[0] != '-') {
		return 0, false
	}
	switch tok[1] {
	case 'r', 'R':
		return fs.DOSReadOnly, true
	case 'h', 'H':
		return fs.DOSHidden, true
	case 's', 'S':
		return fs.DOSSystem, true
	case 'a', 'A':
		return fs.DOSArchive, true
	}
	return 0, false
}

// formatAttrs renders a DOS attribute set as letters.
func formatAttrs(a uint16) string {
	var b strings.Builder
	for _, m := range []struct {
		bit  uint16
		char byte
	}{{fs.DOSReadOnly, 'R'}, {fs.DOSHidden, 'H'}, {fs.DOSSystem, 'S'}, {fs.DOSArchive, 'A'}} {
		if a&m.bit != 0 {
			b.WriteByte(m.char)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// fail prints an error and returns exit code 1.
func fail(err error) int {
	fmt.Fprintln(os.Stderr, "csfs:", err)
	return 1
}
