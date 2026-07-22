package main

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/client/xfer"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

// runREPL opens one session against rawURI and runs an interactive shell holding it,
// so a user browses/copies without re-logging-in per command. Supported:
//
//	ls [path]  cd <path>  pwd  get <path> <host>  put <host> <path>
//	cp <src> <dst>  mv <old> <new>  rm <path>  attrib <path> [±rhsa]
//	type <path> [CODE]  creator <path> [CODE]  help  quit
func runREPL(cfg config, rawURI string) int {
	remote, target, err := connect(cfg, rawURI)
	if err != nil {
		return fail(err)
	}
	defer fs.CloseFS(remote)

	cwd := target.Path // start at the URI's path (usually the volume root)
	fmt.Printf("connected to %s — type 'help' for commands, 'quit' to exit\n", target.Redacted())

	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("%s:/%s> ", target.Scheme, cwd)
		if !sc.Scan() {
			break
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		cmd, rest := fields[0], fields[1:]
		if cmd == "quit" || cmd == "exit" {
			break
		}
		if replCmd(cfg, remote, &cwd, cmd, rest) {
			continue
		}
	}
	return 0
}

// replCmd runs one REPL command against the held session. It returns true always (the
// loop continues); errors are printed, not fatal.
func replCmd(cfg config, remote fs.ForkFS, cwd *string, cmd string, args []string) bool {
	switch cmd {
	case "help":
		fmt.Println("ls [path]  cd <path>  pwd  get <path> <host>  put <host> <path>  cp <src> <dst>  mv <old> <new>  rm <path>  attrib <path> [±rhsa]  type <path> [CODE]  creator <path> [CODE]  quit")
	case "pwd":
		fmt.Printf("/%s\n", *cwd)
	case "cd":
		if len(args) != 1 {
			fmt.Println("usage: cd <path>")
			break
		}
		np := resolveREPLPath(*cwd, args[0])
		if info, err := remote.Stat(np); err != nil || !info.IsDir() {
			fmt.Printf("cd: %s: not a directory\n", np)
			break
		}
		*cwd = np
	case "ls":
		p := *cwd
		if len(args) == 1 {
			p = resolveREPLPath(*cwd, args[0])
		}
		listDir(remote, p)
	case "rm":
		if len(args) != 1 {
			fmt.Println("usage: rm <path>")
			break
		}
		if err := xfer.Remove(remote, resolveREPLPath(*cwd, args[0])); err != nil {
			fmt.Println("rm:", err)
		}
	case "mv":
		if len(args) != 2 {
			fmt.Println("usage: mv <old> <new>")
			break
		}
		if err := xfer.Move(remote, resolveREPLPath(*cwd, args[0]), resolveREPLPath(*cwd, args[1])); err != nil {
			fmt.Println("mv:", err)
		}
	case "get":
		if len(args) != 2 {
			fmt.Println("usage: get <remote-path> <host-path>")
			break
		}
		replGet(cfg, remote, resolveREPLPath(*cwd, args[0]), args[1])
	case "put":
		if len(args) != 2 {
			fmt.Println("usage: put <host-path> <remote-path>")
			break
		}
		replPut(cfg, remote, args[0], resolveREPLPath(*cwd, args[1]))
	case "attrib":
		if len(args) < 1 {
			fmt.Println("usage: attrib <path> [±rhsa]")
			break
		}
		replAttrib(remote, resolveREPLPath(*cwd, args[0]), args[1:])
	case "type", "creator":
		if len(args) < 1 {
			fmt.Printf("usage: %s <path> [CODE]\n", cmd)
			break
		}
		replTypeCreator(remote, resolveREPLPath(*cwd, args[0]), args[1:], cmd == "type")
	default:
		fmt.Printf("unknown command %q (try 'help')\n", cmd)
	}
	return true
}

// resolveREPLPath resolves an argument against the current working dir: an absolute
// (leading '/') path replaces cwd; otherwise it is joined onto cwd. ".." ascends.
func resolveREPLPath(cwd, arg string) string {
	if strings.HasPrefix(arg, "/") {
		return strings.Trim(path.Clean(arg), "/")
	}
	joined := path.Join("/"+cwd, arg)
	return strings.Trim(path.Clean(joined), "/")
}

func replGet(cfg config, remote fs.ForkFS, remotePath, hostPath string) {
	host, err := resolveEndpoint(cfg, hostPath)
	if err != nil {
		fmt.Println("get:", err)
		return
	}
	defer host.close()
	if err := xfer.Copy(remote, host.fsys, remotePath, host.path); err != nil {
		fmt.Println("get:", err)
	}
}

func replPut(cfg config, remote fs.ForkFS, hostPath, remotePath string) {
	host, err := resolveEndpoint(cfg, hostPath)
	if err != nil {
		fmt.Println("put:", err)
		return
	}
	defer host.close()
	if err := xfer.Copy(host.fsys, remote, host.path, remotePath); err != nil {
		fmt.Println("put:", err)
	}
}

func replAttrib(remote fs.ForkFS, p string, toks []string) {
	if len(toks) == 0 {
		attr, _ := remote.Meta().Attrs(p)
		fmt.Println(formatAttrs(attr.Attrs))
		return
	}
	var set, clear uint16
	for _, tok := range toks {
		bit, ok := attrBit(tok)
		if !ok {
			fmt.Printf("bad token %q\n", tok)
			return
		}
		if tok[0] == '+' {
			set |= bit
		} else {
			clear |= bit
		}
	}
	if err := xfer.SetAttr(remote, p, set, clear); err != nil {
		fmt.Println("attrib:", err)
	}
}

func replTypeCreator(remote fs.ForkFS, p string, args []string, isType bool) {
	if len(args) == 0 {
		fi, ok, _ := remote.ReadFinderInfo(p)
		if !ok {
			fmt.Println("(none)")
			return
		}
		if isType {
			fmt.Println(strings.TrimRight(string(fi[0:4]), "\x00 "))
		} else {
			fmt.Println(strings.TrimRight(string(fi[4:8]), "\x00 "))
		}
		return
	}
	var err error
	if isType {
		err = xfer.SetType(remote, p, args[0])
	} else {
		err = xfer.SetCreator(remote, p, args[0])
	}
	if err != nil {
		fmt.Println("set:", err)
	}
}
