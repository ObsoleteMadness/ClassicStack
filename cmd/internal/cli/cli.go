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
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	gort "runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/adapter/config/describe"
	configtoml "github.com/ObsoleteMadness/ClassicStack/adapter/config/toml"
	configuci "github.com/ObsoleteMadness/ClassicStack/adapter/config/uci"
	finderadapter "github.com/ObsoleteMadness/ClassicStack/adapter/control/finder"
	controlhttp "github.com/ObsoleteMadness/ClassicStack/adapter/control/http"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/pcap"
	logbus "github.com/ObsoleteMadness/ClassicStack/adapter/log/bus"
	adaptermetrics "github.com/ObsoleteMadness/ClassicStack/adapter/metrics"
	adapterserial "github.com/ObsoleteMadness/ClassicStack/adapter/serial"
	storefile "github.com/ObsoleteMadness/ClassicStack/adapter/store/file"
	clienttrace "github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/compose/registry"
	"github.com/ObsoleteMadness/ClassicStack/compose/runtime"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/hostinfo"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"

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
	hostinfo.SetBuildInfo(v.Version, v.Commit, v.Date)
	hostinfo.SetBoardInfo("N/A", "N/A", gort.GOARCH)

	fs := flag.NewFlagSet("classicstack", flag.ContinueOnError)
	configPath := fs.String("config", DefaultConfigPath, "path to the config file (TOML, or UCI for an /etc/config path or *.uci file)")
	httpAddr := fs.String("http", "", "override [http] listen address (empty = server.toml, default :1984)")
	showVersion := fs.Bool("version", false, "print version information and exit")
	listIfaces := fs.Bool("list-ifaces", false, "list the capturable pcap NICs (the names an [EtherTalk]/[MacIP]/... interface accepts) and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Printf("classicstack %s\ncommit: %s\nbuilt: %s\ngo: %s\n", v.Version, v.Commit, v.Date, gort.Version())
		return nil
	}
	if *listIfaces {
		printInterfaces(os.Stdout)
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

	// Telemetry bus the supervisor and control plane publish on. Buffer is sized
	// above a full stats flush (one StatSample per component every ~2s) so a burst
	// of samples cannot crowd out log audit lines on the SSE subscriber channel.
	telemetry := bus.New(256)

	// Bus log sink: fans every component (and control-plane) Info+ record onto the
	// telemetry "log" topic so the web-UI / ubus log viewer sees Start/Stop and
	// configuration-change audit lines. Threshold follows [Logging] Level.
	logLevel := registry.ParseLevel(m.Logging.Level)
	logLevelVar := log.NewLevelVar(logLevel)
	busLogSink := logbus.New(telemetry, logLevelVar)
	// Client AFP/FUSE loggers emit Debug; this sink threshold (not the caller)
	// decides whether those lines print. Mute ATP packet trace unless Level is trace.
	clienttrace.SetLevel(logLevel)
	if logLevel > log.Trace {
		clienttrace.SetScope("atp", false)
	}
	clienttrace.AddSink(busLogSink)

	// Build the supervised runtime. The pcap opener + serial opener are injected here
	// so compose/runtime pulls in no cgo/libpcap; under the pcap tag they open real
	// device links, otherwise their stubs return ErrUnavailable and ports come up
	// inert-but-routed. Per-port wire capture is now a port property (Section.Capture),
	// wrapped inside the compose registry openers, so no capture decoration is needed here.
	rt, err := runtime.Build(runtime.Options{
		Model:               m,
		Telemetry:           telemetry,
		Opener:              pcapOpener,
		Serial:              serialOpener,
		InterfaceEnumerator: interfaceEnumerator,
		DefaultDevice:       defaultDevice,
		HostMAC:             hostMAC,
		MacIPEgress:         macipEgressOpener,
		LogSinks:            []log.Sink{busLogSink},
		LogLevel:            logLevelVar,
	})
	if err != nil {
		return fmt.Errorf("build runtime: %w", err)
	}
	rt.Supervisor().SetLogLevelApplier(func(level string) {
		lvl := registry.ParseLevel(level)
		logLevelVar.Set(lvl)
		clienttrace.SetLevel(lvl)
		if lvl > log.Trace {
			clienttrace.SetScope("atp", false)
		} else {
			clienttrace.SetScope("atp", true)
		}
	})

	if err := rt.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "classicstack: start:", err)
	}

	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()

	var restartRequested atomic.Bool

	// Periodically flush per-port pcap capture files so an ungraceful kill (SIGKILL /
	// double-Ctrl-C, which skips the clean-shutdown flush below) loses at most one interval
	// of buffered records instead of the whole in-flight buffer. The clean-shutdown path
	// flushes + closes once more before returning.
	stopFlusher := registry.StartCaptureFlusher(2 * time.Second)
	defer registry.CloseCaptureSinks()
	defer stopFlusher()

	// Optional telemetry export sink: mirrors component stats into expvar for an
	// external scrape (Prometheus / a Windows PerfMon HTTP collector). A no-op unless
	// built with the `perfcounters` tag; it is just one more bus subscriber, so it
	// neither perturbs the producers nor the HTTP/ubus front-ends.
	metricsSink := adaptermetrics.New(telemetry)
	metricsSink.Start()

	// Web-admin control API: [http] in server.toml (default enabled on :1984).
	// -http overrides the listen address and implies enabled.
	var httpServer *controlhttp.Server
	listen := strings.TrimSpace(*httpAddr)
	if listen == "" && m.HTTP.Enabled {
		listen = m.HTTP.ListenAddr()
	}

	var finderSvc *finderadapter.Service
	if c := rt.Component(config.ClientKey); c != nil {
		finderSvc, _ = c.(*finderadapter.Service)
	}

	if listen != "" {
		plane := control.New(rt.Supervisor(), codec, store, telemetry)
		// Management-action logger: stderr + bus so Start/Stop/Restart and config
		// apply/save from any front-end (web UI, ubus) produce Info audit lines both
		// on the console and in the Logs tab.
		plane.SetLogger(log.New("control",
			log.NewStderrSink(logLevelVar),
			busLogSink,
		))
		// Wire the real diagnostics probe surface (zone/routing-table reads) now that the
		// router exists; replaces the core's "unavailable" default. A no-router build
		// passes nil, which keeps the probes reporting ErrUnavailable.
		plane.SetDiagnostics(buildDiagnostics(rt))
		plane.SetSchemaDescriber(describe.All)
		httpServer = controlhttp.NewServer(plane, listen)
		// The protocol-specific diagnostic drill-downs (NBP names, MacIP leases) are served
		// by the diagnostics adapter (which imports the services), NOT through the neutral
		// plane — so core/control carries no protocol type.
		httpServer.SetDiagProvider(buildDiagProvider(rt))
		httpServer.SetFinder(finderSvc)
		httpServer.SetLifecycle(controlhttp.Lifecycle{
			Shutdown: func() {
				fmt.Fprintln(os.Stderr, "classicstack: shutdown requested from web admin")
				stopRun()
			},
			Restart: func() {
				fmt.Fprintln(os.Stderr, "classicstack: restart requested from web admin")
				restartRequested.Store(true)
				stopRun()
			},
		})
		if err := httpServer.Start(); err != nil {
			_ = rt.Stop(context.Background())
			return fmt.Errorf("start web-admin on %s: %w", listen, err)
		}
		fmt.Printf("web-admin control API listening on %s\n", httpServer.Addr())
	}

	<-runCtx.Done()

	fmt.Fprintln(os.Stderr, "classicstack: shutting down")
	metricsSink.Stop()
	if httpServer != nil {
		fmt.Fprintln(os.Stderr, "classicstack: stopping web-admin")
		httpServer.Stop()
	}
	fmt.Fprintln(os.Stderr, "classicstack: stopping runtime")
	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stopErr := rt.Stop(stopCtx)
	if stopErr != nil {
		fmt.Fprintf(os.Stderr, "classicstack: runtime stop: %v\n", stopErr)
	} else {
		fmt.Fprintln(os.Stderr, "classicstack: shutdown complete")
	}
	if restartRequested.Load() {
		if err := relaunchProcess(args); err != nil {
			return fmt.Errorf("restart: %w", err)
		}
	}
	return stopErr
}

// relaunchProcess starts a fresh ClassicStack with the same CLI args and exits the
// current process. Used when the web admin requests a stack restart.
//
// It replays os.Args[1:] rather than the args parameter passed to Run: args is
// the reconstructed flag-only slice used for the initial parse (e.g. just
// "-config <path>"), but the process may have actually been invoked with a
// leading subcommand (classicstackd's "run -config <path>"). Relaunching with
// args would drop that subcommand and make classicstackd's dispatcher reject
// the relaunch as an unknown command, so os.Args is what must be replayed.
func relaunchProcess(_ []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// pickCodec selects the config Codec from the config path: the OpenWRT UCI codec when
// the path is an OpenWRT config (a file under an /etc/config directory, or any *.uci
// file), else the TOML codec. This lets one binary read server.toml on a desktop and
// /etc/config/classicstack on a router with no separate build — the OpenWRT init
// script just points -config at the UCI file. The check is purely on the path string
// (the file need not exist yet — a missing file still boots the default model).
func pickCodec(configPath string) config.Codec {
	// A manual backslash→slash replace, NOT filepath.ToSlash: ToSlash only converts
	// the BUILD platform's own separator, so it is a no-op for a Windows-style
	// "C:\etc\config\classicstack" path on a Linux/macOS build — this classification
	// must work the same regardless of which platform is doing the classifying (an
	// operator can pass a Windows-style -config path to a cross-built binary, and the
	// test suite exercises Windows-shaped paths on every CI runner).
	lower := strings.ToLower(strings.ReplaceAll(configPath, `\`, "/"))
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
// interface via libpcap, programming the caller-supplied BPF filter onto the handle.
// The transport picks the filter (bpf) — EtherTalk captures AppleTalk, NetBEUI captures
// NBF, IPX captures IPX — so a promiscuous handle surfaces only that transport's frames
// to its read loop; a shared filter here previously starved NetBEUI/IPX of their own
// traffic. The low-latency profile (promiscuous, immediate mode, 250ms timeout) suits
// every NIC transport, so we reuse the EtherTalk shape and only swap the filter.
//
// Under `-tags pcap` or `-tags all` this is a real capture handle. WITHOUT those
// tags the stub returns pcap.ErrUnavailable — which we map to (nil, nil) here so
// the port comes up INERT-BUT-ROUTED rather than failing Start and aborting the
// whole runtime. This is the documented degradation (runport.Start treats a nil
// link as a successful inert start): a build with no libpcap should still boot
// its other transports/services, not crash because one port has no backend. A
// genuine open error (device busy / no permission on a pcap build) is still
// propagated. Called per Start so a reopened port gets a fresh handle.
var pcapOpener registry.LinkOpener = func(iface, bpf string) (link.FrameLink, error) {
	cfg := pcap.DefaultEtherTalkConfig(iface)
	cfg.Filter = bpf
	fl, err := pcap.Open(cfg)
	if errors.Is(err, pcap.ErrUnavailable) {
		return nil, nil // no pcap backend in this build → inert, not fatal
	}
	return fl, err
}

// serialOpener is the runtime's SerialOpener: open a serial byte stream for a
// kind="serial" interface (device path + line settings) via adapter/serial. The
// TashTalk factory pairs it with the tashtalk framer (the kind→opener dispatch,
// M11.c/D7). Baud 0 means the adapter default; RTS/CTS is on unless the interface
// opts out (adapter/serial.DefaultRTSCTS). Called per Start.
var serialOpener registry.SerialOpener = func(device string, params registry.SerialParams) (io.ReadWriteCloser, error) {
	return adapterserial.Open(adapterserial.Config{
		Device:        device,
		Baud:          params.Baud,
		NoFlowControl: params.NoFlowControl,
	})
}

// printInterfaces writes the host's capturable pcap NICs to w — the -list-ifaces output
// for classicstack itself, in the same shape client/link.PrintInterfaces gives the file/
// probe clients (a raw device Name, its Description, and any bound IP addresses) so a
// user picks the same device string for a server config's interface as for a client -iface.
func printInterfaces(w io.Writer) {
	devs, err := pcap.ListDevices()
	if err != nil {
		fmt.Fprintf(w, "cannot list interfaces: %v\n", err)
		fmt.Fprintln(w, "(raw-Ethernet transports need a build with the 'pcap' tag and Npcap/libpcap installed)")
		return
	}
	if len(devs) == 0 {
		fmt.Fprintln(w, "no capturable interfaces found")
		return
	}
	fmt.Fprintln(w, "Interfaces:")
	for _, d := range devs {
		desc := d.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(w, "  %s\n      %s", d.Name, desc)
		if len(d.Addresses) > 0 {
			fmt.Fprintf(w, " [%s]", strings.Join(d.Addresses, ", "))
		}
		fmt.Fprintln(w)
	}
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
		// Name stays the RAW pcap device (what a config must store); the friendly
		// adaptor description goes in a separate field the picker shows as a label but
		// never stores — otherwise "\Device\NPF_{GUID} (Realtek …)" gets saved as the
		// device and pcap cannot open it.
		out = append(out, control.InterfaceInfo{Name: d.Name, Description: d.Description, Addr: addr})
	}
	return out, nil
}

// defaultDevice resolves the host's PRIMARY (default-route) NIC to the pcap device name a
// NIC port opens when its interface names none — the server "Easy mode" auto-NIC threaded
// into every BuildContext. It enumerates the pcap devices, then lets core/hostinfo pick
// the one bound to the routing-table primary interface (pcap-free, cross-platform, no
// privileges; only an IP match bridges an OS interface to Npcap's "\Device\NPF_{GUID}").
// Under the pcap tag it resolves a real device; the stub's ListDevices errors and this
// returns that error, which nicLinkOpener treats as "no auto-NIC" → inert-but-routed.
func defaultDevice() (string, error) {
	hd, err := pcapHostDevices()
	if err != nil {
		return "", err
	}
	pick, err := hostinfo.PrimaryDevice(hd)
	if err != nil {
		return "", err
	}
	return pick.Name, nil
}

// hostMAC resolves the real hardware address of a pcap device so NIC ports that leave
// mac / hw_address blank stamp the host NIC's own MAC (WiFi APs drop any other source).
func hostMAC(device string) ([6]byte, error) {
	// pcap.ListDevices can fail (no pcap tag, no permission) while the OS still
	// knows the NIC — fall through to InterfaceByName via an empty device list.
	hd, err := pcapHostDevices()
	if err != nil {
		hd = nil
	}
	return hostinfo.HardwareAddrForDevice(device, hd)
}

// pcapHostDevices maps adapter/link/pcap's device list to the pcap-free hostinfo.Device
// view PrimaryDevice / HardwareAddrForDevice consume.
func pcapHostDevices() ([]hostinfo.Device, error) {
	devs, err := pcap.ListDevices()
	if err != nil {
		return nil, err
	}
	hd := make([]hostinfo.Device, len(devs))
	for i, d := range devs {
		hd[i] = hostinfo.Device{Name: d.Name, Addresses: d.Addresses}
	}
	return hd, nil
}
