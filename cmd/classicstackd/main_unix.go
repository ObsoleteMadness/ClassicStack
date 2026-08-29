//go:build !windows

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/buildinfo"
	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/cli"
)

const (
	// defaultPIDFile is where the daemon records its child PID.
	defaultPIDFile = "/var/run/classicstack.pid"
	// defaultLogFile receives the detached daemon's stdout/stderr.
	defaultLogFile = "/var/log/classicstack.log"
)

func main() {
	version := cli.Version{Version: BuildVersion, Commit: BuildCommit, Date: BuildDate}

	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	cmd := strings.ToLower(args[0])
	if err := dispatch(cmd, args[1:], version); err != nil {
		fmt.Fprintf(os.Stderr, "classicstackd %s: %v\n", cmd, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `classicstackd — run ClassicStack as a background daemon

Usage:
  classicstackd start  -config <path> [-pidfile <p>] [-log <p>]   daemonize
  classicstackd stop          [-pidfile <p>]                      stop the daemon
  classicstackd status        [-pidfile <p>]                      report liveness
  classicstackd run    -config <path>                             run in the foreground
  classicstackd install -config <path> [-log <p>]                 macOS: login item (LaunchAgent)
  classicstackd uninstall                                         macOS: remove the LaunchAgent
  classicstackd version                                           print version information
`)
}

// dispatch routes a verb to its handler.
func dispatch(cmd string, args []string, version cli.Version) error {
	switch cmd {
	case "start":
		return cmdStart(args)
	case "stop":
		return cmdStop(args)
	case "status":
		return cmdStatus(args)
	case "run":
		return cmdRun(args, version)
	case "install":
		return cmdInstall(args)
	case "uninstall", "remove":
		return cmdUninstall(args)
	case "version":
		buildinfo.Print(os.Stdout, "classicstackd", version.Version, version.Commit, version.Date)
		return nil
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// startFlags parses the flags shared by start/install.
type daemonFlags struct {
	config  string
	pidFile string
	logFile string
}

func parseFlags(name string, args []string, withConfig bool) (daemonFlags, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	cfg := fs.String("config", "", "Path to the TOML config file")
	pid := fs.String("pidfile", defaultPIDFile, "Path to the PID file")
	logf := fs.String("log", defaultLogFile, "Path to the daemon log file")
	if err := fs.Parse(args); err != nil {
		return daemonFlags{}, err
	}
	out := daemonFlags{config: *cfg, pidFile: *pid, logFile: *logf}
	if withConfig && strings.TrimSpace(out.config) == "" {
		return daemonFlags{}, errors.New("-config <path> is required")
	}
	if out.config != "" {
		abs, err := filepath.Abs(out.config)
		if err != nil {
			return daemonFlags{}, err
		}
		out.config = abs
	}
	return out, nil
}

// cmdRun runs the stack in the foreground, exactly like `classicstack
// -config <path>`, stopping gracefully on SIGINT/SIGTERM.
func cmdRun(args []string, version cli.Version) error {
	f, err := parseFlags("run", args, true)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.Run(ctx, []string{"-config", f.config}, version)
}

// cmdStart re-execs this binary as `run -config <cfg>` in a new session,
// detached from the controlling terminal, with output redirected to the log
// file, and records the child PID.
func cmdStart(args []string) error {
	f, err := parseFlags("start", args, true)
	if err != nil {
		return err
	}

	if pid, alive := readPID(f.pidFile); alive {
		return fmt.Errorf("already running (pid %d, %s)", pid, f.pidFile)
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating executable: %w", err)
	}

	logFD, err := os.OpenFile(f.logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening log file %s: %w", f.logFile, err)
	}
	defer func() { _ = logFD.Close() }()

	cmd := exec.Command(self, "run", "-config", f.config)
	cmd.Stdin = nil
	cmd.Stdout = logFD
	cmd.Stderr = logFD
	// New session so the child has no controlling terminal and survives the
	// parent shell exiting (the classic daemonize step).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting daemon: %w", err)
	}

	// Capture the PID before Release: os.Process.Release sets Pid to -1.
	childPID := cmd.Process.Pid

	if err := writePID(f.pidFile, childPID); err != nil {
		// Best effort: kill the child we just spawned since we cannot track it.
		_ = cmd.Process.Kill()
		return fmt.Errorf("writing PID file %s: %w", f.pidFile, err)
	}

	// Release the child so it keeps running after this process exits.
	_ = cmd.Process.Release()
	fmt.Printf("started classicstackd (pid %d), logging to %s\n", childPID, f.logFile)
	return nil
}

// cmdStop sends SIGTERM to the recorded PID and waits briefly for exit.
func cmdStop(args []string) error {
	f, err := parseFlags("stop", args, false)
	if err != nil {
		return err
	}
	pid, alive := readPID(f.pidFile)
	if pid == 0 {
		return fmt.Errorf("no PID file at %s", f.pidFile)
	}
	if !alive {
		_ = os.Remove(f.pidFile)
		return fmt.Errorf("not running (stale PID %d removed)", pid)
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("signalling pid %d: %w", pid, err)
	}
	// Wait for the process to exit, up to a timeout.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			_ = os.Remove(f.pidFile)
			fmt.Printf("stopped classicstackd (pid %d)\n", pid)
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for pid %d to exit", pid)
}

// cmdStatus reports whether the recorded PID is alive.
func cmdStatus(args []string) error {
	f, err := parseFlags("status", args, false)
	if err != nil {
		return err
	}
	pid, alive := readPID(f.pidFile)
	switch {
	case pid == 0:
		fmt.Println("classicstackd: not running (no PID file)")
	case alive:
		fmt.Printf("classicstackd: running (pid %d)\n", pid)
	default:
		fmt.Printf("classicstackd: not running (stale PID %d)\n", pid)
	}
	return nil
}

// readPID returns the PID recorded in the file and whether that process is
// alive. A missing/empty file yields (0, false).
func readPID(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, pidAlive(pid)
}

// pidAlive reports whether a process with the given PID exists, using the
// signal-0 liveness probe.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// On Unix, signal 0 performs error checking without sending a signal.
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	// EPERM means the process exists but we lack permission to signal it.
	return errors.Is(err, syscall.EPERM)
}

func writePID(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o644)
}
