//go:build webui || all

package registry

import (
	"strings"

	finderadapter "github.com/ObsoleteMadness/ClassicStack/adapter/control/finder"
	clienttrace "github.com/ObsoleteMadness/ClassicStack/client/trace"
	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

func init() {
	buildClientHook = buildClient
	// Auto-mounted FUSE volumes round-trip with the in-process client (same tag).
	config.RegisterFUSEVolumes()
	// The in-process file client (LAN scan, remote sessions, FUSE/WinFsp mounts) is
	// a supervised component like ports and routers. It is built in a second runtime
	// pass once file services exist so LocalVolumes can resolve live shares.
	Register(config.ClientKey, func(ctx *BuildContext) (component.Component, error) {
		c, _, err := buildClient(ctx, ctx.Components)
		return c, err
	})
}

func buildClient(ctx *BuildContext, comps map[string]component.Component) (component.Component, bool, error) {
	if ctx == nil || ctx.Model == nil {
		return nil, false, nil
	}
	logger := buildClientLogger(ctx)
	src := &finderadapter.RuntimeSource{
		Comps:       comps,
		ConfigModel: ctx.Model,
	}
	svc := finderadapter.New(src, logger)
	if ctx.Telemetry != nil {
		svc.SetPublisher(ctx.Telemetry)
	}
	return svc, true, nil
}

func buildClientLogger(ctx *BuildContext) log.Logger {
	var min *log.LevelVar
	if ctx != nil && ctx.LogLevel != nil {
		min = ctx.LogLevel
	} else {
		min = log.NewLevelVar(ctx.LevelFor())
	}
	sinks := []log.Sink{log.NewStderrSink(min)}
	sinks = append(sinks, ctx.LogSinks...)
	if path := strings.TrimSpace(ctx.Model.Client.LogFile); path != "" {
		fsink, err := log.NewFileSink(path, min)
		if err != nil {
			log.New("client").Log2(log.Warn, "client log file unreadable",
				log.Str("path", path), log.Str("err", err.Error()))
		} else {
			sinks = append(sinks, fsink)
			clienttrace.AddSink(fsink)
		}
	}
	return log.New("finder", sinks...)
}
