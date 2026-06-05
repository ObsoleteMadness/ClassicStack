//go:build !webui && !all

package app

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
)

type webUIHookDisabled struct{}

func (webUIHookDisabled) Start(_ context.Context) error { return nil }
func (webUIHookDisabled) Stop() error                   { return nil }

// wireWebUI is the no-op build. It warns if the operator asked for the
// web UI but the binary was built without -tags webui.
func wireWebUI(w WebUIWiring) (WebUIHook, error) {
	if w.Options.Enabled {
		netlog.Warn("[MAIN][WebUI] -webui-enabled set but binary was built without -tags webui; ignoring")
	}
	return webUIHookDisabled{}, nil
}
