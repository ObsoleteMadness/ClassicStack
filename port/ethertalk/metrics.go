package ethertalk

import "github.com/ObsoleteMadness/ClassicStack/pkg/telemetry"

var aarpProbeRetriesTotal = telemetry.NewCounter("classicstack_aarp_probe_retries_total")
