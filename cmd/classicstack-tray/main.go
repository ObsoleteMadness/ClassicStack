//go:build darwin || windows

// Command classicstack-tray is the menu bar / system tray app for
// ClassicStack: a status item that reports whether the ClassicStack process
// is running, and offers Open Interface plus Start / Restart / Shutdown
// against the existing web-admin control API (adapter/control/http),
// depending on whether it's currently running. Quit only closes the tray
// app — ClassicStack keeps running; use Shutdown to actually stop it.
//
// This file holds the platform-independent menu/state-machine logic. Each
// OS supplies: startDaemon/daemonPath (launcher_*.go — how the underlying
// process gets started), loadCredentials/saveCredentials/forgetCredentials/
// promptCredentials/showAlert (credentials_*.go — credential storage and
// native dialogs), trayIconPNG (icon_*.go), openInterface and recoveryHint
// (open_*.go). On macOS this is packaged into ClassicStack.app alongside
// classicstackd — see scripts/package-app-darwin.sh and `make app-darwin`.
// On Windows it drives classicstack-svc.exe — see README.md.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"fyne.io/systray"
)

const (
	pollInterval  = 5 * time.Second
	actionTimeout = 8 * time.Second
)

func main() {
	httpAddr := flag.String("http", "", "control API address to monitor (empty = server.toml default, :1984)")
	flag.Parse()

	client := newControlClient(controlBaseURL(*httpAddr))
	if user, pass, ok := loadCredentials(); ok {
		client.setAuth(user, pass)
	}
	systray.Run(onReady(client), onExit)
}

// menu bundles the items whose visibility/label changes with stack state, so
// syncMenu has one place to keep them consistent.
type menu struct {
	status   *systray.MenuItem
	start    *systray.MenuItem
	restart  *systray.MenuItem
	shutdown *systray.MenuItem
}

// syncMenu updates the Status label and shows exactly the actions that make
// sense for the current state: only Start when stopped, only Restart/Shutdown
// otherwise (running, or running-but-needs-setup).
func (m menu) syncMenu(state stackState) {
	switch state {
	case stateRunning:
		m.status.SetTitle("Status: Running")
	case stateSetupRequired:
		m.status.SetTitle("Status: Running (complete setup via Open Interface)")
	default:
		m.status.SetTitle("Status: Stopped")
	}

	if state == stateStopped {
		m.restart.Hide()
		m.shutdown.Hide()
		m.start.Show()
	} else {
		m.start.Hide()
		m.restart.Show()
		m.shutdown.Show()
	}
}

func onReady(client *controlClient) func() {
	return func() {
		systray.SetIcon(trayIconPNG)
		systray.SetTitle("")
		systray.SetTooltip("ClassicStack")

		mStatus := systray.AddMenuItem("Status: checking…", "Current ClassicStack status")
		mStatus.Disable()
		systray.AddSeparator()
		mOpen := systray.AddMenuItem("Open Interface", "Open the ClassicStack web admin UI")
		systray.AddSeparator()
		mStart := systray.AddMenuItem("Start ClassicStack", "Start the ClassicStack process")
		mRestart := systray.AddMenuItem("Restart ClassicStack", "Restart the ClassicStack process")
		mShutdown := systray.AddMenuItem("Shutdown ClassicStack", "Stop the ClassicStack process")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Quit the menu bar app (ClassicStack keeps running)")

		m := menu{status: mStatus, start: mStart, restart: mRestart, shutdown: mShutdown}
		m.syncMenu(stateStopped) // hide Restart/Shutdown until the first real check lands

		refresh := make(chan struct{}, 1)
		triggerRefresh := func() {
			select {
			case refresh <- struct{}{}:
			default:
			}
		}

		initialState := client.status()
		if initialState == stateStopped {
			go func() {
				start(client)
				triggerRefresh()
			}()
		}

		go statusLoop(client, m, refresh)

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					openInterface(client.baseURL)
				case <-mStart.ClickedCh:
					go func() {
						start(client)
						triggerRefresh()
					}()
				case <-mRestart.ClickedCh:
					performAction(client, "Restart", client.restart)
					triggerRefresh()
				case <-mShutdown.ClickedCh:
					stop(client, "Shutdown")
					triggerRefresh()
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}
}

// start launches the daemon and waits for the control API to answer,
// reporting failure to start or to come up in time.
func start(client *controlClient) {
	if err := startDaemon(); err != nil {
		fmt.Fprintf(os.Stderr, "classicstack-tray: start failed: %v\n", err)
		showAlert("ClassicStack", fmt.Sprintf("Start failed: %v", err))
		return
	}
	if !client.waitUntilRunning(actionTimeout) {
		showAlert("ClassicStack", "ClassicStack was started but isn't answering yet — "+recoveryHint())
	}
}

// stop issues Shutdown (with the credential dance in performAction) and then
// verifies the process actually went down, since handleShutdown stops it
// asynchronously and a 200 response only means the request was accepted.
func stop(client *controlClient, verb string) {
	if !performAction(client, verb, client.shutdown) {
		return
	}
	if !client.waitUntilStopped(actionTimeout) {
		showAlert("ClassicStack", "ClassicStack did not stop — something may be relaunching it. "+recoveryHint())
	}
}

// statusLoop keeps the menu in sync, polling on a timer and whenever refresh
// is signalled (right after an action, so the menu reacts faster than the
// next tick).
func statusLoop(client *controlClient, m menu, refresh <-chan struct{}) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	check := func() { m.syncMenu(client.status()) }
	check()
	for {
		select {
		case <-ticker.C:
			check()
		case <-refresh:
			check()
		}
	}
}

// performAction runs a Restart/Shutdown control call, handling the admin
// credential the control API requires once one is configured
// (adapter/control/http/auth.go authGate): try any cached credential first,
// then prompt once on a 401 and retry, saving a credential that works and
// discarding one that doesn't. Reports whether the request was ultimately
// accepted (not whether its effect has finished — see stop()'s follow-up
// verification for Shutdown).
func performAction(client *controlClient, verb string, action func() error) bool {
	err := action()
	if err == nil {
		return true
	}
	if errors.Is(err, errSetupRequired) {
		showAlert("ClassicStack", fmt.Sprintf("%s failed: complete initial setup via Open Interface first.", verb))
		return false
	}
	if !errors.Is(err, errUnauthorized) {
		fmt.Fprintf(os.Stderr, "classicstack-tray: %s failed: %v\n", verb, err)
		showAlert("ClassicStack", fmt.Sprintf("%s failed: %v", verb, err))
		return false
	}

	user, pass, ok := promptCredentials(fmt.Sprintf("ClassicStack needs its admin credentials to %s.", verb))
	if !ok {
		return false // user cancelled
	}
	client.setAuth(user, pass)

	if err := action(); err != nil {
		if errors.Is(err, errUnauthorized) {
			forgetCredentials()
			showAlert("ClassicStack", "Incorrect ClassicStack admin username or password.")
		} else {
			fmt.Fprintf(os.Stderr, "classicstack-tray: %s failed: %v\n", verb, err)
			showAlert("ClassicStack", fmt.Sprintf("%s failed: %v", verb, err))
		}
		return false
	}
	if err := saveCredentials(user, pass); err != nil {
		fmt.Fprintf(os.Stderr, "classicstack-tray: %v\n", err)
	}
	return true
}

func onExit() {}
