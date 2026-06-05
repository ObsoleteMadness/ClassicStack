//go:build windows

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"

	"github.com/ObsoleteMadness/ClassicStack/internal/app"
)

// acceptedControls are the SCM control requests the service responds to:
// Stop and Shutdown both trigger a graceful teardown.
const acceptedControls = svc.AcceptStop | svc.AcceptShutdown

// serviceHandler implements svc.Handler. It runs the ClassicStack run-core
// (internal/app) in a goroutine and translates SCM Stop/Shutdown requests
// into context cancellation so the existing graceful shutdown path runs.
type serviceHandler struct {
	cfgPath string
	version app.Version
	elog    *eventlog.Log
}

// Execute is invoked by svc.Run. It reports StartPending → Running, launches
// app.Run, and waits for either the stack to exit on its own or an SCM
// Stop/Shutdown, in which case it cancels the context and waits for app.Run
// to return before reporting Stopped.
func (h *serviceHandler) Execute(_ []string, r <-chan svc.ChangeRequest, s chan<- svc.Status) (bool, uint32) {
	const cmdsAccepted = acceptedControls

	s <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- app.Run(ctx, runArgs(h.cfgPath), h.version)
	}()

	s <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
	h.info(1, "ClassicStack service running")

	for {
		select {
		case err := <-runErr:
			// The stack exited on its own (a fatal build/config error, or it
			// returned after ctx was cancelled). Report the outcome to the SCM.
			if err != nil {
				h.error(1, "ClassicStack exited with error: "+err.Error())
				s <- svc.Status{State: svc.Stopped, Win32ExitCode: 1}
				return false, 1
			}
			s <- svc.Status{State: svc.Stopped}
			return false, 0

		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				s <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				h.info(1, "ClassicStack service stopping")
				s <- svc.Status{State: svc.StopPending}
				cancel()
				// Wait for app.Run to finish its graceful Supervisor.Stop.
				<-runErr
				s <- svc.Status{State: svc.Stopped}
				return false, 0
			default:
				// Ignore controls we did not advertise.
			}
		}
	}
}

func (h *serviceHandler) info(eid uint32, msg string) {
	if h.elog != nil {
		_ = h.elog.Info(eid, msg)
	}
}

func (h *serviceHandler) error(eid uint32, msg string) {
	if h.elog != nil {
		_ = h.elog.Error(eid, msg)
	}
}

// signalContext returns a context cancelled on Ctrl-C / SIGTERM, for the
// console-run fallback in runService.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
