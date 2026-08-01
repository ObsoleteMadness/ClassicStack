//go:build esp32 && wt32eth01

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
	_ "github.com/ObsoleteMadness/ClassicStack/hardware/peripherals/sdcard"
)

const (
	ConfigPath = "server.toml"

	// WT32-ETH01 Pin mapping
	PHY_MDC   = 23
	PHY_MDIO  = 18
	PHY_POWER = 16
	PHY_ADDR  = 1

	UART_TXD = 17
	UART_RXD = 5
	UART_CTS = 35
)

// Build metadata injected at link time
var (
	BuildVersion = "0.0.0-dev"
	BuildCommit  = "unknown"
	BuildDate    = "unknown"
)

func main() {
	time.Sleep(2 * time.Second) // Allow hardware to stabilize
	println("--- ClassicStack WT32-ETH01 Booting ---")

	hostinfo.SetBoardInfo("WT32-ETH01", "LAN8720A", "xtensa")
	hostinfo.SetBuildInfo(BuildVersion, BuildCommit, BuildDate)

	// 1. Initialize the LAN8720A PHY
	println("Initializing LAN8720A PHY...")
	phyDriver := lan8720a.New(machine.Pin(PHY_MDC), machine.Pin(PHY_MDIO), machine.Pin(PHY_POWER), PHY_ADDR)
	if err := phyDriver.Init(); err != nil {
		println("Error initializing LAN8720A PHY:", err.Error())
	} else {
		println("LAN8720A PHY initialized successfully.")
	}

	// 2. Load Configuration
	println("Loading configuration...")
	store := storefile.New(ConfigPath)
	codec := configtoml.New()
	m, err := runtime.Load(store, codec)
	if err != nil {
		println("Error loading config, using default:", err.Error())
		m = config.NewModel()
	}

	// 3. Initialize WiFi (if configured)
	var wifi *WiFi
	var wifiIP string
	for _, iface := range m.Interfaces {
		if iface.EffectiveKind() == config.IfaceKindWifi && iface.SSID != "" {
			println("Initializing WiFi connecting to SSID:", iface.SSID)
			wifi = NewWiFi(iface.SSID, iface.Key)
			dhcp := iface.Proto == "" || iface.Proto == "dhcp"
			err := wifi.Connect(dhcp, iface.IP, iface.Netmask, iface.Gateway)
			if err != nil {
				println("WiFi connection failed:", err.Error())
			} else {
				wifiIP, _ = wifi.GetIP()
				println("WiFi connected successfully. IP:", wifiIP)
				hostinfo.SetHostNetworkInfo(wifiIP, "N/A")
			}
			break
		}
	}

	// 4. Setup Custom Openers for the Supervisor
	telemetry := bus.New(32)

	// Custom LinkOpener to return the raw L2 EMAC FrameLink. The BPF filter arg is
	// ignored — an embedded EMAC link has no kernel filter; the port read loops demux
	// in userland (the graceful-degradation path an empty/unsupported filter takes).
	opener := func(iface, _ string) (link.FrameLink, error) {
		println("Opening EMAC raw L2 FrameLink for interface:", iface)
		return OpenEMAC(PHY_MDC, PHY_MDIO, PHY_POWER, PHY_ADDR)
	}

	// Custom SerialOpener for TashTalk UART (Secondary UART at 1MBaud with CTS)
	serialOpener := func(device string, baud uint) (io.ReadWriteCloser, error) {
		println("Opening UART for TashTalk at 1MBaud...")
		uart := machine.UART1
		err := uart.Configure(machine.UARTConfig{
			BaudRate: 1000000,
			TX:       machine.Pin(UART_TXD),
			RX:       machine.Pin(UART_RXD),
		})
		if err != nil {
			return nil, err
		}

		// Configure CTS input pin
		ctsPin := machine.Pin(UART_CTS)
		ctsPin.Configure(machine.PinConfig{Mode: machine.PinInput})

		// Wrap UART with custom flow-control if necessary, or return standard UART
		return uart, nil
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

	// Keep main goroutine alive and periodically print status
	for {
		time.Sleep(10 * time.Second)
		linkInfo := phyDriver.GetLinkInfo()
		status := "DOWN"
		if linkInfo.Up {
			status = fmt.Sprintf("UP (%d Mbps, %s-Duplex)", linkInfo.Speed, map[bool]string{true: "Full", false: "Half"}[linkInfo.Duplex])
		}
		println("Status - Ethernet:", status, "| WiFi IP:", wifiIP)
	}
}
