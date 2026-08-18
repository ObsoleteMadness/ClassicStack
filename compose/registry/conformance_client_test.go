//go:build webui || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

func init() {
	conformanceStagers[config.ClientKey] = func(m *config.Model) {
		m.Client.Enabled = true
	}
}
