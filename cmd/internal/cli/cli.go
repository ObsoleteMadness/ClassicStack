// Package cli is the shared run-core for every classicstack binary in the new ring:
// the interactive cmd/classicstack(-ng), the Windows service wrapper, and the Unix
// daemon all call Main/Run here so the load → build → supervise → serve-control →
// teardown loop lives in ONE place (the new-ring replacement for the legacy
// internal/app run-core). It lives under cmd/ because it is the COMPOSE EDGE: it
// chooses the concrete adapters — the file config Store, the TOML Codec, the pcap
// LinkOpener, the serial opener, the HTTP control server — that compose/runtime
// deliberately does NOT import (so the runtime ring stays adapter-agnostic and
// cgo-free). Keeping it out of compose/runtime is the ring split the design requires.
//
// Run is config-file driven (TOML): the per-transport flag surface the legacy
// run-core carried is superseded by server.toml + the web-admin control plane (the
// named-instance config model). Run accepts only the cross-cutting flags every entry
// point shares — -config, -http, -version — and the service/daemon wrappers pass
// "-config <path>" just as they did to the legacy app.Run.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	gort "runtime"
	"strings"
	"syscall"
	"time"

	configtoml "github.com/ObsoleteMadness/ClassicStack/adapter/config/toml"
	configuci "github.com/ObsoleteMadness/ClassicStack/adapter/config/uci"
	controlhttp "github.com/ObsoleteMadness/ClassicStack/adapter/control/http"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/pcap"
	adaptermetrics "github.com/ObsoleteMadness/ClassicStack/adapter/metrics"
	adapterserial "github.com/ObsoleteMadness/ClassicStack/adapter/serial"
	storefile "github.com/ObsoleteMadness/ClassicStack/adapter/store/file"
	"github.com/ObsoleteMadness/ClassicStack/compose/registry"
	"github.com/ObsoleteMadness/ClassicStack/compose/runtime"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/link"

	// Blank-import the components so their build-tagged init()s self-register with the
	// compose registry (the §8 replacement for *_disabled.go). A component whose build
	// tag is absent never registers, so the supervisor simply cannot build it. Every
	// binary that embeds this run-core gets the same registered set; the active subset
	// is chosen at build time via tags (e.g. -tags all, or -tags "afp smb pcap").
	_ "github.com/ObsoleteMadness/ClassicStack/compose/registry" // tag stub
	_ "github.com/ObsoleteMadness/ClassicStack/core/port/ethertalk"
	_ "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	_ "github.com/ObsoleteMadness/ClassicStack/core/port/localtalk"
	_ "github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"
	_ "github.com/ObsoleteMadness/ClassicStack/core/router"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/afp"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/browser"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/macip"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/messenger"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

// DefaultConfigPath is the config file Run loads when -config is not given.
const DefaultConfigPath = "server.toml"

// Version carries the link-time build metadata the binaries inject (-ldflags -X).
// It mirrors the legacy app.Version so the service/daemon wrappers thread the same
// struct through unchanged.
type Version struct {
	Version string
	Commit  string
	Date    string
}

// Main is the interactive entry point: derive a context cancelled on SIGINT/SIGTERM
// (the foreground Ctrl-C behaviour) and run the stack until it fires. cmd/classicstack
// calls this; the service/daemon wrappers call Run directly with a context they cancel
// on the SCM/daemon stop signal.
func Main(v Version) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := Run(ctx, os.Args[1:], v); err != nil {
		fmt.Fprintln(os.Stderr, "classicstack:", err)
		os.Exit(1)
	}
}

// Run parses the shared flags, loads server.toml, builds the supervised runtime,
// optionally serves the web-admin control API, starts the stack, and blocks until
// ctx is cancelled — then tears it down. It is the shared run-core the interactive
// Main and the service/daemon wrappers both invoke. -version short-circuits with a
// nil error after printing.
func Run(ctx context.Context, args []string, v Version) error {
	fs := flag.NewFlagSet("classicstack", flag.ContinueOnError)
	configPath := fs.String("config", DefaultConfigPath, "path to the config file (TOML, or UCI for an /etc/config path or *.uci file)")
	httpAddr := fs.String("http", "", "serve the web-admin control API on this address (e.g. :8080); empty = disabled")
	showVersion := fs.Bool("version", false, "print version information and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Printf("classicstack %s\ncommit: %s\nbuilt: %s\ngo: %s\n", v.Version, v.Commit, v.Date, gort.Version())
		return nil
	}

	// Config model: file Store + a Codec chosen by the config path at this (compose)
	// edge — TOML by default, UCI when the path is an OpenWRT config (under /etc/config
	// or a *.uci file), so the SAME binary reads server.toml on a desktop and
	// /etc/config/classicstack on a router. A missing file yields the default model, so
	// the stack still boots with no config present.
	store := storefile.New(*configPath)
	codec := pickCodec(*configPath)
	m, err := runtime.Load(store, codec)
	if err != nil {
		return fmt.Errorf("load %s: %w", *configPath, err)
	}

	// Telemetry bus the supervisor and control plane publish on.
	telemetry := bus.New(32)

	// Build the supervised runtime. The pcap opener + serial opener are injected here
	// so compose/runtime pulls in no cgo/libpcap; under the pcap tag they open real
	// device links, otherwise their stubs return ErrUnavailable and ports come up
	// inert-but-routed. When [Capture] names a pcap file for an interface, the opener is
	// wrapped so that interface's frames are tee'd to the file (link.Capture decorator).
	opener := captureOpener(pcapOpener, &m.Capture)
	rt, err := runtime.Build(runtime.Options{Model: m, Telemetry: telemetry, Opener: opener, Serial: serialOpener, InterfaceEnumerator: interfaceEnumerator, MacIPEgress: macipEgressOpener})
	if err != nil {
		return fmt.Errorf("build runtime: %w", err)
	}

	if err := rt.Start(ctx); err != nil {
		return fmt.Errorf("start runtime: %w", err)
	}

	// Optional telemetry export sink: mirrors component stats into expvar for an
	// external scrape (Prometheus / a Windows PerfMon HTTP collector). A no-op unless
	// built with the `perfcounters` tag; it is just one more bus subscriber, so it
	// neither perturbs the producers nor the HTTP/ubus front-ends.
	metricsSink := adaptermetrics.New(telemetry)
	metricsSink.Start()

	// Web-admin control API (opt-in via -http): a control.Plane over the supervisor +
	// the same Store/Codec, exposed through the new-ring HTTP control adapter.
	var httpServer *controlhttp.Server
	if *httpAddr != "" {
		plane := control.New(rt.Supervisor(), codec, store, telemetry)
		// Wire the real diagnostics probe surface (zone/routing-table reads) now that the
		// router exists; replaces the core's "unavailable" default. A no-router build
		// passes nil, which keeps the probes reporting ErrUnavailable.
		plane.SetDiagnostics(buildDiagnostics(rt))
		httpServer = controlhttp.NewServer(plane, *httpAddr)
		if err := httpServer.Start(); err != nil {
			_ = rt.Stop(context.Background())
			return fmt.Errorf("start web-admin on %s: %w", *httpAddr, err)
		}
		fmt.Printf("web-admin control API listening on %s\n", httpServer.Addr())
	}

	<-ctx.Done()

	metricsSink.Stop()
	if httpServer != nil {
		httpServer.Stop()
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return rt.Stop(stopCtx)
}

// pickCodec selects the config Codec from the config path: the OpenWRT UCI codec when
// the path is an OpenWRT config (a file under an /etc/config directory, or any *.uci
// file), else the TOML codec. This lets one binary read server.toml on a desktop and
// /etc/config/classicstack on a router with no separate build — the OpenWRT init
// script just points -config at the UCI file. The check is purely on the path string
// (the file need not exist yet — a missing file still boots the default model).
func pickCodec(configPath string) config.Codec {
	lower := strings.ToLower(filepath.ToSlash(configPath))
	switch {
	case strings.HasSuffix(lower, ".uci"),
		strings.HasSuffix(lower, ".config"),     // the repo's openwrt/files/classicstack.config
		strings.Contains(lower, "/etc/config/"): // the installed UCI path on a router
		return configuci.New()
	default:
		return configtoml.New()
	}
}

// pcapOpener is the runtime's LinkOpener: open a raw Ethernet FrameLink for a port's
// interface via libpcap (the low-latency EtherTalk profile). Under the pcap tag this
// is a real capture handle; without it the stub returns pcap.ErrUnavailable and the
// port stays inert. Called per Start so a reopened port gets a fresh handle.
var pcapOpener registry.LinkOpener = func(iface string) (link.FrameLink, error) {
	return pcap.Open(pcap.DefaultEtherTalkConfig(iface))
}

// serialOpener is the runtime's SerialOpener: open a serial byte stream for a
// kind="serial" interface (device path + baud) via adapter/serial. The TashTalk
// factory pairs it with the tashtalk framer (the kind→opener dispatch, M11.c/D7).
// baud 0 means the adapter default. Called per Start.
var serialOpener registry.SerialOpener = func(device string, baud uint) (io.ReadWriteCloser, error) {
	return adapterserial.Open(adapterserial.Config{Device: device, Baud: baud})
}

// interfaceEnumerator lists the host NICs for the control plane's ListInterfaces (the
// UI's NIC picker), mapping the pcap device list to control.InterfaceInfo. Under the
// pcap tag it enumerates real devices; the stub returns an error, which surfaces as an
// empty list. Injected into the runtime so the supervisor stays pcap-free.
func interfaceEnumerator() ([]control.InterfaceInfo, error) {
	devs, err := pcap.ListDevices()
	if err != nil {
		// No pcap backend in this build (the stub) or no permission: an empty NIC list
		// is the right degradation for the UI dropdown, not a propagated error.
		return nil, nil
	}
	out := make([]control.InterfaceInfo, 0, len(devs))
	for _, d := range devs {
		addr := ""
		if len(d.Addresses) > 0 {
			addr = d.Addresses[0]
		}
		name := d.Name
		if d.Description != "" {
			name = d.Name + " (" + d.Description + ")"
		}
		out = append(out, control.InterfaceInfo{Name: name, Addr: addr})
	}
	return out, nil
}
