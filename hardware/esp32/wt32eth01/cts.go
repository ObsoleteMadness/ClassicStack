//go:build esp32 && wt32eth01

package main

import (
	"io"
	"machine"
	"time"
)

// Software RTS/CTS for TashTalk on boards whose UART exposes no hardware flow
// control. The desktop build gets the same behaviour for free from the OS driver
// (adapter/serial sets RTSCTSFlowControl; see adapter/serial.DefaultRTSCTS), and the
// reference implementation tashrouter opens its port with rtscts=True. It matters
// because TashTalk accepts host bytes at 1 Mbit/s but clocks them onto LocalTalk at
// 230.4 kbaud: without back-pressure its receive buffer overruns mid-frame and the
// truncated LLAP frame simply fails FCS and disappears.
const (
	// ctsAssertedLow: TashTalk drives CTS LOW to mean "clear to send" (the RS-232
	// convention the adapter follows), so a HIGH pin means stop.
	ctsAssertedLow = true
	// ctsPollInterval is how long to wait before re-reading a de-asserted CTS. At
	// 230.4 kbaud one LocalTalk byte takes ~35us, so 100us re-checks promptly while
	// leaving the scheduler room.
	ctsPollInterval = 100 * time.Microsecond
	// ctsChunk is the number of bytes written between CTS checks. TashTalk's buffer
	// is small, so re-check often; 1 is the safe floor for correctness.
	ctsChunk = 1
	// ctsMaxWait bounds a single stall so a disconnected or unwired CTS line cannot
	// block the write loop forever — it degrades to unthrottled writes instead.
	ctsMaxWait = 50 * time.Millisecond
)

// ctsWriter wraps a UART, gating writes on the CTS input pin. Reads pass straight
// through: flow control here only throttles host→TashTalk traffic, which is the
// direction that overruns.
type ctsWriter struct {
	uart *machine.UART
	cts  machine.Pin
}

// newCTSWriter returns rw wrapped so Write blocks while CTS is de-asserted.
func newCTSWriter(uart *machine.UART, cts machine.Pin) io.ReadWriteCloser {
	return &ctsWriter{uart: uart, cts: cts}
}

// clearToSend reports whether TashTalk is currently accepting bytes.
func (w *ctsWriter) clearToSend() bool {
	high := w.cts.Get()
	if ctsAssertedLow {
		return !high
	}
	return high
}

// waitClear blocks until CTS is asserted or ctsMaxWait elapses. A timeout returns
// anyway (writing regardless) so an unwired CTS line degrades to no flow control
// rather than a permanent stall.
func (w *ctsWriter) waitClear() {
	deadline := time.Now().Add(ctsMaxWait)
	for !w.clearToSend() {
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(ctsPollInterval)
	}
}

// Write sends p in small chunks, waiting for CTS before each one.
func (w *ctsWriter) Write(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		end := total + ctsChunk
		if end > len(p) {
			end = len(p)
		}
		w.waitClear()
		n, err := w.uart.Write(p[total:end])
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

func (w *ctsWriter) Read(p []byte) (int, error) { return w.uart.Read(p) }

// Close is a no-op: the UART is a fixed peripheral, not a closable handle.
func (w *ctsWriter) Close() error { return nil }
