//go:build afp || all

package afp

import "github.com/ObsoleteMadness/ClassicStack/pkg/telemetry"

var afpCommandsTotal = telemetry.NewCounter("classicstack_afp_commands_total")
