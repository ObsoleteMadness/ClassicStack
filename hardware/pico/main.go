//go:build pico || pico2

package main

import (
	"context"
	"fmt"
	"io"
	"machine"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/compose/registry"
	"github.com/ObsoleteMadness/ClassicStack/compose/runtime"
	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/link"

	configtoml "github.com/ObsoleteMadness/ClassicStack/adapter/config/toml"
	controlhttp "github.com/ObsoleteMadness/ClassicStack/adapter/control/http"
	logbus "github.com/ObsoleteMadness/ClassicStack/adapter/log/bus"
	storefile "github.com/ObsoleteMadness/ClassicStack/adapter/store/file"
	"github.com/ObsoleteMadness/ClassicStack/core/control"
	"github.com/ObsoleteMadness/ClassicStack/core/hostinfo"
	"github.com/ObsoleteMadness/ClassicStack/core/log"

	"github.com/ObsoleteMadness/ClassicStack/hardware/peripherals/lan8720a"
	// hardware/peripherals/sdcard is disabled here: it imports tinygo.org/x/drivers/fatfs,
	// which does not exist in any released tinygo.org/x/drivers version, and the only real
	// TinyGo FAT implementation found (tinygo.org/x/tinyfs/fatfs) is a cgo binding — cgo is
	// not usable on TinyGo's baremetal ESP32/RP2040 targets. Re-enable once a genuine
	// pure-Go (or TinyGo-cgo-capable) FAT driver backs it; until then this target has no
	// SD-card fs_type.
	// _ "github.com/ObsoleteMadness/ClassicStack/hardware/peripherals/sdcard"
)

const (
	ConfigPath = "server.toml"

	// Pico Pin Mapping
	UART_TXD = 0
	UART_RXD = 1
	UART_CTS = 2

	SD_CLK  = 6
	SD_MOSI = 7
	SD_MISO = 4
	SD_CS   = 5

	ETH_MDC    = 14
	ETH_MDIO   = 15
	ETH_REFCLK = 20
)

// Build metadata injected at link time
var (
	BuildVersion = "0.0.0-dev"
	BuildCommit  = "unknown"
	BuildDate    = "unknown"
)

func main() {
	time.Sleep(2 * time.Second) // Allow hardware to stabilize
	println("--- ClassicStack Raspberry Pi Pico Booting ---")

	hostinfo.SetBuildInfo(BuildVersion, BuildCommit, BuildDate)

	// 1. Load Configuration First
	println("Loading configuration...")
	store := storefile.New(ConfigPath)
	codec := configtoml.New()
	m, err := runtime.Load(store, codec)
	if err != nil {
		println("Error loading config, using default:", err.Error())
		m = config.NewModel()
	}

	// Determine configured Ethernet controller
	ethernetController := "lan8720" // default
	for _, iface := range m.Interfaces {
		if iface.EffectiveKind() == config.IfaceKindNIC {
			if iface.Controller != "" {
				ethernetController = iface.Controller
			}
			break
		}
	}

	ethType := "LAN8720A"
	if ethernetController == "w5500" {
		ethType = "W5500"
	}
	hostinfo.SetBoardInfo("Pi Pico", ethType, "arm")

	// 2. Initialize the LAN8720A PHY (only if configured)
	var phyDriver *lan8720a.Driver
	if ethernetController == "lan8720" {
		println("Initializing LAN8720A PHY...")
		phyDriver = lan8720a.New(machine.Pin(ETH_MDC), machine.Pin(ETH_MDIO), machine.NoPin, 1)
		if err := phyDriver.Init(); err != nil {
			println("Error initializing LAN8720A PHY:", err.Error())
		} else {
			println("LAN8720A PHY initialized successfully.")
		}
	} else {
		println("Using W5500 SPI-based Ethernet (skipping LAN8720A initialization).")
	}

	// 3. Initialize WiFi (if Pico W / Pico 2 W and configured)
	var wifiIP string
	setupWiFi(m, &wifiIP)
	if wifiIP != "" {
		hostinfo.SetHostNetworkInfo(wifiIP, "N/A")
	}

	// 4. Setup Custom Openers for the Supervisor
	telemetry := bus.New(32)

	// Custom LinkOpener selecting the controller at runtime. The BPF filter arg is
	// ignored — an embedded SPI/PIO Ethernet link has no kernel filter; the port read
	// loops demux in userland (the empty/unsupported-filter degradation path).
	opener := func(iface, _ string) (link.FrameLink, error) {
		if ethernetController == "w5500" {
			println("Opening SPI-based W5500 Ethernet FrameLink for interface:", iface)
			return OpenW5500Ethernet()
		}
		println("Opening PIO-based RMII LAN8720 Ethernet FrameLink for interface:", iface)
		return OpenLAN8720Ethernet(ETH_MDC, ETH_MDIO, ETH_REFCLK)
	}

	// Custom SerialOpener for TashTalk UART (UART0 at 1MBaud with CTS)
	serialOpener := func(device string, params registry.SerialParams) (io.ReadWriteCloser, error) {
		println("Opening UART0 for TashTalk at 1MBaud...")
		uart := machine.UART0
		err := uart.Configure(machine.UARTConfig{
			BaudRate: 1000000,
			TX:       machine.Pin(UART_TXD),
			RX:       machine.Pin(UART_RXD),
		})
		if err != nil {
			return nil, err
		}
		if params.NoFlowControl {
			return uart, nil
		}

		// Configure CTS input pin and gate writes on it: TinyGo's UART exposes no
		// hardware RTS/CTS, so ctsWriter polls the pin in software (see cts.go).
		ctsPin := machine.Pin(UART_CTS)
		ctsPin.Configure(machine.PinConfig{Mode: machine.PinInput})
		return newCTSWriter(uart, ctsPin), nil
	}

	// Bus log sink: fans component + control-plane Info+ records onto the telemetry
	// "log" topic so the web-UI Logs tab sees Start/Stop and config audit lines.
	logLevel := registry.ParseLevel(m.Logging.Level)
	busLogSink := logbus.New(telemetry, log.NewLevelVar(logLevel))

	// 5. Build and Start the Supervisor
	println("Building ClassicStack runtime...")
	rt, err := runtime.Build(runtime.Options{
		Model:     m,
		Telemetry: telemetry,
		Opener:    opener,
		Serial:    serialOpener,
		LogSinks:  []log.Sink{busLogSink},
	})
	if err != nil {
		println("Fatal: failed to build runtime:", err.Error())
		return
	}

	ctx := context.Background()
	println("Starting ClassicStack services...")
	if err := rt.Start(ctx); err != nil {
		println("Fatal: failed to start runtime:", err.Error())
		return
	}

	// 6. Start Web UI
	println("Starting Web UI on :8080...")
	plane := control.New(rt.Supervisor(), codec, store, telemetry)
	plane.SetLogger(log.New("control", busLogSink))
	httpServer := controlhttp.NewServer(plane, ":8080")
	if err := httpServer.Start(); err != nil {
		println("Error starting Web UI:", err.Error())
	} else {
		println("Web UI listening on", httpServer.Addr())
	}

	println("ClassicStack is running!")

	// Keep main goroutine alive
	for {
		time.Sleep(10 * time.Second)
		status := "UP (SPI)"
		if ethernetController == "lan8720" && phyDriver != nil {
			linkInfo := phyDriver.GetLinkInfo()
			status = "DOWN"
			if linkInfo.Up {
				status = fmt.Sprintf("UP (%d Mbps, %s-Duplex)", linkInfo.Speed, map[bool]string{true: "Full", false: "Half"}[linkInfo.Duplex])
			}
		}
		if wifiIP != "" {
			println("Status - Ethernet:", status, "| WiFi IP:", wifiIP)
		} else {
			println("Status - Ethernet:", status)
		}
	}
}
