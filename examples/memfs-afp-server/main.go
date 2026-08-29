// Command memfs-afp-server is a minimal, standalone AFP file server: one
// AppleTalk node with no router services (no RTMP/ZIP), serving a single
// read-only in-memory volume, "MemFS", entirely defined in Go. It is the
// worked example for ClassicStack's "Extending ClassicStack — server SDK"
// documentation (docs/manual.md) — a Mac running AppleShare plugged into an
// existing AppleTalk network, not a router node. It shows three things an
// embedder needs: implementing fs.FileSystem from scratch (memfs.go),
// registering it via fs.RegisterFS (also memfs.go), and wiring a transport +
// router + NBP + AFP service by hand (this file and transport*.go) instead of
// through compose/registry's config-driven assembly.
//
// Run it, then connect from a real (or emulated) classic Mac on the same
// LocalTalk/EtherTalk segment: the volume appears in the Chooser under the
// zone named by -zone. Or, from another machine running ClassicStack's client
// tools, mount afp://guest@<name>:<zone>/MemFS.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/router"
	"github.com/ObsoleteMadness/ClassicStack/core/service/afp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/nbp"
)

func main() {
	transport := flag.String("transport", "ltoudp", "link transport: ltoudp (default, no root needed) or pcap")
	iface := flag.String("iface", "", "ltoudp: local IPv4 address to bind (\"\" = every interface); pcap: NIC device name")
	zone := flag.String("zone", seedZone, "AppleTalk zone MemFS is advertised in")
	flag.Parse()

	logger := log.New("memfs-afp-server", log.NewStderrSink(log.NewLevelVar(log.Info)))
	ctx := context.Background()

	rtr := router.New(logger)
	if err := rtr.Start(ctx); err != nil {
		fatal("router start: %v", err)
	}

	var (
		port router.RoutedPort
		err  error
	)
	switch *transport {
	case "ltoudp", "":
		port, err = openLToUDP(*iface, rtr, logger)
	case "pcap":
		port, err = openPcap(*iface, rtr, logger)
	default:
		fatal("unknown -transport %q (want ltoudp or pcap)", *transport)
	}
	if err != nil {
		fatal("open %s transport: %v", *transport, err)
	}
	// Attach must come after Start: the router rejects membership changes while
	// stopped.
	if err := rtr.Attach(port); err != nil {
		fatal("attach port: %v", err)
	}
	if err := port.Start(ctx); err != nil {
		fatal("start port: %v", err)
	}

	nbpSvc := nbp.New(rtr, logger)
	rtr.RegisterService(nbpSvc)
	if err := nbpSvc.Start(ctx); err != nil {
		fatal("start nbp: %v", err)
	}

	afpSvc, err := afp.NewWithVolumes(logger, afp.VolumeSpec{
		Name:  "MemFS",
		Share: fs.ShareSpec{FSType: memExampleFSType},
	})
	if err != nil {
		fatal("build afp volumes: %v", err)
	}
	afpSvc.SetRouter(rtr)
	afpSvc.SetServerName("MemFS Demo")
	// SetZone is NOT optional here: with no ZIP service populating the router's
	// zone table, the NBP name-registration fallback (router.Zones().Zones()[0])
	// finds nothing and silently skips registering the AFPServer name — the
	// volume would still serve a client that already knows its address, but
	// never appear in a Chooser scan. See core/service/afp/afp.go's
	// registerNBPLocked and nbp_test.go's SetZone regression test.
	afpSvc.SetZone(*zone)
	afpSvc.SetNBP(nbpSvc)
	rtr.RegisterService(afpSvc)
	if err := afpSvc.Start(ctx); err != nil {
		fatal("start afp: %v", err)
	}

	fmt.Printf("memfs-afp-server: MemFS is up on %s, zone %q. Look for it in the Chooser or mount afp://guest@<name>:%s/MemFS\n",
		*transport, *zone, *zone)

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()

	fmt.Println("memfs-afp-server: shutting down")
	_ = afpSvc.Stop(ctx)
	_ = nbpSvc.Stop(ctx)
	_ = port.Stop(ctx)
	_ = rtr.Stop(ctx)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "memfs-afp-server: "+format+"\n", args...)
	os.Exit(1)
}
