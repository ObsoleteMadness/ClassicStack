package registry

import (
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// ParseLevel maps a [Logging] Level string ("trace"|"debug"|"info"|"warn"|"error")
// to a log.Level, defaulting to Info for an empty or unrecognised value. It is the
// single place the config's textual level becomes the sink threshold, so every
// component agrees on what "debug" means. Case-insensitive; leading/trailing spaces
// are ignored.
func ParseLevel(s string) log.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return log.Trace
	case "debug":
		return log.Debug
	case "warn", "warning":
		return log.Warn
	case "error":
		return log.Error
	case "info", "":
		return log.Info
	default:
		return log.Info
	}
}

// LevelFor returns the shared log threshold a factory should build its sink with,
// resolved from the model's [Logging] Level (ParseLevel). Threading it through the
// BuildContext is what makes `[Logging] Level='debug'` actually reach every
// component's logger, instead of the hard-coded Info each factory previously used.
// A nil ctx or model falls back to Info.
func (ctx *BuildContext) LevelFor() log.Level {
	if ctx == nil || ctx.Model == nil {
		return log.Info
	}
	return ParseLevel(ctx.Model.Logging.Level)
}

// Logger builds a component logger writing to stderr at the configured level, plus
// any extra sinks the cmd edge installed (BuildContext.LogSinks, e.g. the web-UI ring
// buffer). scope is the component/instance name shown on each record. This is the one
// constructor every factory uses so verbosity is honoured uniformly (§6b): the stderr
// sink's LevelVar is seeded from [Logging] Level, and the same records fan out to the
// extra sinks at their own thresholds.
func (ctx *BuildContext) Logger(scope string) log.Logger {
	lvl := ctx.LevelFor()
	sinks := []log.Sink{log.NewStderrSink(log.NewLevelVar(lvl))}
	if ctx != nil {
		sinks = append(sinks, ctx.LogSinks...)
	}
	return log.New(scope, sinks...)
}
