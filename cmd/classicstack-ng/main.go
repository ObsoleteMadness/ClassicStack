package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/compose/registry"
	"github.com/ObsoleteMadness/ClassicStack/compose/supervisor"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"

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
	fmt.Println("Starting ClassicStack-NG (Phase 1 Skeleton Main)...")

	// 1. Initialize config model and enable placeholder ports
	m := config.NewModel()
	m.Set(&port.Section{SKey: "EtherTalk", Iface: "eth0", IsEnabled: true})
	m.Set(&port.Section{SKey: "LocalTalk", Iface: "lt0", IsEnabled: true})
	m.Set(&port.Section{SKey: "IPX", Iface: "ipx0", IsEnabled: true})
	m.Set(&port.Section{SKey: "NetBEUI", Iface: "netbeui0", IsEnabled: true})

	// 2. Build telemetry bus and supervisor
	telemetry := bus.New(32)
	sup := supervisor.New(m, telemetry)

	// Subscribe to telemetry state changed events so we can print transitions
	stateCh, cancel := telemetry.Subscribe(bus.TopicState)
	defer cancel()

	go func() {
		for ev := range stateCh {
			if sc, ok := ev.(bus.StateChanged); ok {
				fmt.Printf("[TELEMETRY] Component %q transitioned: %s -> %s\n", sc.Component, sc.From, sc.To)
			}
		}
	}()

	// 3. Build and register all registered components
	names := registry.Names()
	fmt.Printf("Registered components in build: %v\n", names)

	for _, name := range names {
		if name == "stub-tagged" || name == "stub-a" || name == "stub-disabled" {
			continue
		}
		comp, ok, err := registry.Build(name, m)
		if err != nil {
			fmt.Printf("Failed to build component %q: %v\n", name, err)
			os.Exit(1)
		}
		if ok && comp != nil {
			// Wire dependencies to demonstrate ordering cascade:
			// AFP service depends on the Router, SMB depends on NetBEUI.
			var deps []string
			if name == "AFP" {
				deps = []string{"Router"}
			} else if name == "SMB" {
				deps = []string{"NetBEUI"}
			}
			sup.Add(comp, deps)
			fmt.Printf("Added component %q with dependencies %v\n", name, deps)
		} else if !ok {
			fmt.Printf("Component %q requested but not registered in this build.\n", name)
		}
	}

	// 4. Start all components
	ctx, cancelStart := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStart()

	fmt.Println("Starting supervisor tree...")
	if err := sup.StartAll(ctx); err != nil {
		fmt.Printf("Error starting supervisor: %v\n", err)
		os.Exit(1)
	}

	// 5. Print status
	fmt.Println("\n--- Supervisor Status ---")
	units := sup.Status()
	for _, unit := range units {
		fmt.Printf("- Unit %q (Enabled: %v, Running: %v, Binding: %q, DependsOn: %v)\n",
			unit.Name, unit.Enabled, unit.Running, unit.Binding, unit.DependsOn)
	}
	fmt.Println("-------------------------")

	// 6. Setup OS signal handler for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("ClassicStack-NG is running. Press Ctrl+C to stop...")
	<-sigCh

	// 7. Stop all components gracefully
	fmt.Println("\nStopping supervisor tree...")
	ctxStop, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStop()
	if err := sup.StopAll(ctxStop); err != nil {
		fmt.Printf("Error stopping supervisor: %v\n", err)
	}

	fmt.Println("ClassicStack-NG stopped.")
}
