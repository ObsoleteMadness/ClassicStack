package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	configtoml "github.com/ObsoleteMadness/ClassicStack/adapter/config/toml"
	controlhttp "github.com/ObsoleteMadness/ClassicStack/adapter/control/http"
	"github.com/ObsoleteMadness/ClassicStack/adapter/link/pcap"
	adapterserial "github.com/ObsoleteMadness/ClassicStack/adapter/serial"
	storefile "github.com/ObsoleteMadness/ClassicStack/adapter/store/file"
	"github.com/ObsoleteMadness/ClassicStack/compose/registry"
	"github.com/ObsoleteMadness/ClassicStack/compose/runtime"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/link"

	// Blank-import components to trigger registry self-registration via init()
	_ "github.com/ObsoleteMadness/ClassicStack/compose/registry" // tag stub
	_ "github.com/ObsoleteMadness/ClassicStack/core/port/ethertalk"
	_ "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	_ "github.com/ObsoleteMadness/ClassicStack/core/port/localtalk"
	_ "github.com/ObsoleteMadness/ClassicStack/core/port/netbeui"
	_ "github.com/ObsoleteMadness/ClassicStack/core/router"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/afp"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/macip"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/netbios"
	_ "github.com/ObsoleteMadness/ClassicStack/core/service/smb"
)

func main() {
	httpAddr := flag.String("http", "", "serve the web-admin control API on this address (e.g. :8080); empty = disabled")
	flag.Parse()

	fmt.Println("Starting ClassicStack-NG...")

	// 1. Config model: load server.toml (file Store + TOML Codec). A missing file
	//    yields defaults (runtime.Load returns the default model for an empty store),
	//    so the ng harness still boots with no config present. The Store/Codec are
	//    chosen HERE at the cmd edge; compose/runtime stays adapter-agnostic.
	const configPath = "server.toml"
	store := storefile.New(configPath)
	codec := configtoml.New()
	m, err := runtime.Load(store, codec)
	if err != nil {
		fmt.Printf("Failed to load %s: %v\n", configPath, err)
		os.Exit(1)
	}

	// 2. Telemetry bus + a subscriber that narrates state transitions.
	telemetry := bus.New(32)
	stateCh, cancel := telemetry.Subscribe(bus.TopicState)
	defer cancel()
	go func() {
		for ev := range stateCh {
			if sc, ok := ev.(bus.StateChanged); ok {
				fmt.Printf("[TELEMETRY] %s: %s -> %s\n", sc.Component, sc.From, sc.To)
			}
		}
	}()

	// 3. Build the runtime: every registered component, dependency-ordered, supervised.
	//    The pcap opener is selected HERE at the cmd edge: under the `pcap` build tag
	//    it is the real libpcap link, otherwise the stub returns ErrUnavailable and
	//    ports come up inert. The runtime/compose packages stay free of cgo because
	//    the opener is injected, not imported by them.
	rt, err := runtime.Build(runtime.Options{Model: m, Telemetry: telemetry, Opener: pcapOpener, Serial: serialOpener})
	if err != nil {
		fmt.Printf("Failed to build runtime: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Built components: %v\n", rt.Built())

	// 4. Start the stack.
	startCtx, cancelStart := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStart()
	fmt.Println("Starting supervisor tree...")
	if err := rt.Start(startCtx); err != nil {
		fmt.Printf("Error starting runtime: %v\n", err)
		os.Exit(1)
	}

	// 5. Status snapshot.
	fmt.Println("\n--- Supervisor Status ---")
	for _, unit := range rt.Supervisor().Status() {
		fmt.Printf("- %q (enabled=%v running=%v binding=%q deps=%v)\n",
			unit.Name, unit.Enabled, unit.Running, unit.Binding, unit.DependsOn)
	}
	fmt.Println("-------------------------")

	// 6. Web-admin control API (opt-in via -http): bind a control.Plane over the
	//    runtime's supervisor + the same Store/Codec the config loaded through, and
	//    expose it through the new-ring HTTP control adapter (JSON API + SSE). The
	//    Plane is the single contract every front-end shares (§14); the SPA (M8-spa)
	//    layers over this same surface. Disabled when -http is empty.
	var httpServer *controlhttp.Server
	if *httpAddr != "" {
		plane := control.New(rt.Supervisor(), codec, store, telemetry)
		httpServer = controlhttp.NewServer(plane, *httpAddr)
		if err := httpServer.Start(); err != nil {
			fmt.Printf("Failed to start web-admin on %s: %v\n", *httpAddr, err)
			os.Exit(1)
		}
		fmt.Printf("Web-admin control API listening on %s\n", httpServer.Addr())
	}

	// 7. Run until interrupted.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	fmt.Println("ClassicStack-NG is running. Press Ctrl+C to stop...")
	<-sigCh

	// 8. Graceful shutdown: stop the web-admin first (so no control call races the
	//    teardown), then the supervisor tree in reverse dependency order.
	if httpServer != nil {
		httpServer.Stop()
	}
	fmt.Println("\nStopping supervisor tree...")
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStop()
	if err := rt.Stop(stopCtx); err != nil {
		fmt.Printf("Error stopping runtime: %v\n", err)
	}
	fmt.Println("ClassicStack-NG stopped.")
}

// pcapOpener is the runtime's LinkOpener: it opens a raw Ethernet FrameLink for a
// port's configured interface via libpcap (the low-latency EtherTalk profile).
// Under the `pcap` build tag this is a real capture handle; without it the stub
// returns pcap.ErrUnavailable and the port stays inert. It is called per Start so
// a reopened port gets a fresh handle.
var pcapOpener registry.LinkOpener = func(iface string) (link.FrameLink, error) {
	return pcap.Open(pcap.DefaultEtherTalkConfig(iface))
}

// serialOpener is the runtime's SerialOpener: it opens a serial byte stream for a
// `kind = "serial"` interface (device path + baud) via adapter/serial. The TashTalk
// factory pairs it with the tashtalk framer (the kind→opener dispatch, M11.c/D7), so
// the device-open lives here at the cmd edge while the framing stays in the adapter.
// baud 0 means the adapter default (1 Mbit/s for TashTalk). Called per Start.
var serialOpener registry.SerialOpener = func(device string, baud uint) (io.ReadWriteCloser, error) {
	return adapterserial.Open(adapterserial.Config{Device: device, Baud: baud})
}
