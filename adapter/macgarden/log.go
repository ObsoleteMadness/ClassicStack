//go:build macgarden || all

package macgarden

// log.go is a thin printf-style logging shim over core/log, so the MacGarden scraper
// (ported from the legacy netlog.Info/Warn/Debug printf API) keeps its call sites
// unchanged. The package logs under the "MacGarden" scope to a stderr sink at Info; a
// host that wants the records on the management bus can SetLogger with its own.

import (
	"fmt"
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

var (
	logMu  sync.RWMutex
	logger = log.New("MacGarden", log.NewStderrSink(log.NewLevelVar(log.Info)))
)

// SetLogger replaces the package logger (e.g. with one wired to the telemetry bus). A
// nil logger is ignored. Safe for concurrent use.
func SetLogger(l log.Logger) {
	if l == nil {
		return
	}
	logMu.Lock()
	logger = l
	logMu.Unlock()
}

func curLogger() log.Logger {
	logMu.RLock()
	defer logMu.RUnlock()
	return logger
}

func logInfo(format string, args ...any)  { curLogger().Log(log.Info, fmt.Sprintf(format, args...)) }
func logWarn(format string, args ...any)  { curLogger().Log(log.Warn, fmt.Sprintf(format, args...)) }
func logDebug(format string, args ...any) { curLogger().Log(log.Debug, fmt.Sprintf(format, args...)) }
