package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/compose/runtime"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"

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
	fmt.Println("Starting ClassicStack-NG...")

	// 1. Config model. A TOML/UCI-driven load via runtime.Load(store, codec) is the
	//    M10 cutover; for now the ng harness starts from defaults. (Real device-link
	//    injection is also M10 — every port still comes up inert until then.)
	m := config.NewModel()

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
	rt, err := runtime.Build(runtime.Options{Model: m, Telemetry: telemetry})
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

	// 6. Run until interrupted.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	fmt.Println("ClassicStack-NG is running. Press Ctrl+C to stop...")
	<-sigCh

	// 7. Graceful shutdown.
	fmt.Println("\nStopping supervisor tree...")
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStop()
	if err := rt.Stop(stopCtx); err != nil {
		fmt.Printf("Error stopping runtime: %v\n", err)
	}
	fmt.Println("ClassicStack-NG stopped.")
}
