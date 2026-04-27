//go:build afp

package afp

import "github.com/pgodw/omnitalk/pkg/telemetry"

var afpCommandsTotal = telemetry.NewCounter("omnitalk_afp_commands_total")
