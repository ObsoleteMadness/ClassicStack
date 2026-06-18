//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/ObsoleteMadness/ClassicStack/cmd/internal/cli"
)

const (
	// serviceName is the SCM key and eventlog source name.
	serviceName = "ClassicStack"
	// serviceDisplay is the friendly name shown in services.msc.
	serviceDisplay = "ClassicStack AppleTalk Router"
	// serviceDesc is the SCM description text.
	serviceDesc = "AppleTalk Phase 2 router and classic LAN services (AFP, SMB, NetBIOS)."
)

func main() {
	version := cli.Version{Version: BuildVersion, Commit: BuildCommit, Date: BuildDate}

	// When the SCM launches the service it runs the binary with no extra
	// arguments; svc.IsWindowsService() detects that session so a bare
	// invocation does the right thing.
	if isService, err := svc.IsWindowsService(); err == nil && isService {
		if rerr := runService("", version); rerr != nil {
			fmt.Fprintf(os.Stderr, "classicstack-svc: %v\n", rerr)
			os.Exit(1)
		}
		return
	}

	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	cmd := strings.ToLower(args[0])
	rest := args[1:]
	if err := dispatch(cmd, rest, version); err != nil {
		fmt.Fprintf(os.Stderr, "classicstack-svc %s: %v\n", cmd, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `classicstack-svc — run ClassicStack as a Windows service

Usage:
  classicstack-svc install -config <path>   register the service (auto-start)
  classicstack-svc uninstall                remove the service
  classicstack-svc start                    start the registered service
  classicstack-svc stop                     stop the registered service
  classicstack-svc status                   report the service state
  classicstack-svc run -config <path>       run in this console (debugging)
`)
}

// dispatch routes a verb to its handler. -config is parsed inline (only
// install/run consume it).
func dispatch(cmd string, args []string, version cli.Version) error {
	switch cmd {
	case "install":
		cfg, err := configArg(args)
		if err != nil {
			return err
		}
		return install(cfg)
	case "uninstall", "remove":
		return uninstall()
	case "start":
		return controlStart()
	case "stop":
		return controlStop()
	case "status":
		return status()
	case "run":
		cfg, _ := configArg(args) // empty is allowed (server.toml auto-load)
		return runService(cfg, version)
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// configArg extracts the value of a "-config <path>" pair from args and
// returns it as an absolute path.
func configArg(args []string) (string, error) {
	for i := range args {
		a := args[i]
		switch {
		case a == "-config" || a == "--config":
			if i+1 >= len(args) {
				return "", fmt.Errorf("-config requires a path")
			}
			return filepath.Abs(args[i+1])
		case strings.HasPrefix(a, "-config="):
			return filepath.Abs(strings.TrimPrefix(a, "-config="))
		case strings.HasPrefix(a, "--config="):
			return filepath.Abs(strings.TrimPrefix(a, "--config="))
		}
	}
	return "", nil
}

// install registers the service with the SCM, pointing its image at this
// executable's "run -config <cfg>" so the SCM restarts the right binary.
func install(cfgPath string) error {
	if cfgPath == "" {
		return fmt.Errorf("install requires -config <path>")
	}
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating executable: %w", err)
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connecting to service manager (run as Administrator): %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	if s, err := m.OpenService(serviceName); err == nil {
		_ = s.Close()
		return fmt.Errorf("service %q already exists", serviceName)
	}

	s, err := m.CreateService(serviceName, exePath, mgr.Config{
		DisplayName:  serviceDisplay,
		Description:  serviceDesc,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}, "run", "-config", cfgPath)
	if err != nil {
		return fmt.Errorf("creating service: %w", err)
	}
	defer func() { _ = s.Close() }()

	// Register an eventlog source so Execute can write start/stop entries.
	if err := eventlog.InstallAsEventCreate(serviceName, eventlog.Info|eventlog.Warning|eventlog.Error); err != nil {
		// Non-fatal: the service still runs, it just logs to stderr only.
		fmt.Fprintf(os.Stderr, "warning: registering eventlog source: %v\n", err)
	}

	fmt.Printf("installed service %q (config %s)\n", serviceName, cfgPath)
	return nil
}

// uninstall stops (if running) and removes the service and its eventlog
// source.
func uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connecting to service manager (run as Administrator): %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed", serviceName)
	}
	defer func() { _ = s.Close() }()

	// Best-effort stop before delete so the binary is not in use.
	if st, err := s.Query(); err == nil && st.State != svc.Stopped {
		_, _ = s.Control(svc.Stop)
	}
	if err := s.Delete(); err != nil {
		return fmt.Errorf("deleting service: %w", err)
	}
	_ = eventlog.Remove(serviceName)
	fmt.Printf("removed service %q\n", serviceName)
	return nil
}

func controlStart() error {
	s, m, err := openService()
	if err != nil {
		return err
	}
	defer func() { _ = m.Disconnect() }()
	defer func() { _ = s.Close() }()
	if err := s.Start(); err != nil {
		return fmt.Errorf("starting service: %w", err)
	}
	fmt.Printf("started service %q\n", serviceName)
	return nil
}

func controlStop() error {
	s, m, err := openService()
	if err != nil {
		return err
	}
	defer func() { _ = m.Disconnect() }()
	defer func() { _ = s.Close() }()
	st, err := s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("stopping service: %w", err)
	}
	// Wait briefly for the stop to take effect.
	timeout := time.Now().Add(20 * time.Second)
	for st.State != svc.Stopped {
		if time.Now().After(timeout) {
			return fmt.Errorf("timed out waiting for service to stop")
		}
		time.Sleep(300 * time.Millisecond)
		if st, err = s.Query(); err != nil {
			return fmt.Errorf("querying service: %w", err)
		}
	}
	fmt.Printf("stopped service %q\n", serviceName)
	return nil
}

func status() error {
	s, m, err := openService()
	if err != nil {
		return err
	}
	defer func() { _ = m.Disconnect() }()
	defer func() { _ = s.Close() }()
	st, err := s.Query()
	if err != nil {
		return fmt.Errorf("querying service: %w", err)
	}
	fmt.Printf("service %q: %s\n", serviceName, stateString(st.State))
	return nil
}

func openService() (*mgr.Service, *mgr.Mgr, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to service manager (run as Administrator): %w", err)
	}
	s, err := m.OpenService(serviceName)
	if err != nil {
		_ = m.Disconnect()
		return nil, nil, fmt.Errorf("service %q is not installed", serviceName)
	}
	return s, m, nil
}

func stateString(s svc.State) string {
	switch s {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "start pending"
	case svc.StopPending:
		return "stop pending"
	case svc.Running:
		return "running"
	case svc.ContinuePending:
		return "continue pending"
	case svc.PausePending:
		return "pause pending"
	case svc.Paused:
		return "paused"
	default:
		return fmt.Sprintf("state %d", uint32(s))
	}
}

// runService runs the stack under the SCM via svc.Run. cfgPath may be empty
// (server.toml auto-load). When not running under the SCM (console run for
// debugging) svc.Run fails, so we fall back to running the stack directly.
func runService(cfgPath string, version cli.Version) error {
	h := &serviceHandler{cfgPath: cfgPath, version: version}

	elog, err := eventlog.Open(serviceName)
	if err == nil {
		h.elog = elog
		defer func() { _ = elog.Close() }()
	}

	if err := svc.Run(serviceName, h); err != nil {
		// Likely launched from a console rather than the SCM: run the stack
		// in the foreground so `run` is still useful for debugging.
		fmt.Fprintf(os.Stderr, "not started by the SCM (%v); running in the foreground\n", err)
		return runForeground(cfgPath, version)
	}
	return nil
}

// runForeground runs the stack with a signal-cancelled context. It is split
// out so the os.Exit in runService's caller does not skip the signal-context
// cleanup (the deferred stop runs when this function returns).
func runForeground(cfgPath string, version cli.Version) error {
	ctx, stop := signalContext()
	defer stop()
	return cli.Run(ctx, runArgs(cfgPath), version)
}

// runArgs builds the argument slice handed to cli.Run from the config path.
func runArgs(cfgPath string) []string {
	if cfgPath == "" {
		return nil
	}
	return []string{"-config", cfgPath}
}
