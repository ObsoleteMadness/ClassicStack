package ethertalk

import "github.com/pgodw/omnitalk/pkg/telemetry"

var aarpProbeRetriesTotal = telemetry.NewCounter("omnitalk_aarp_probe_retries_total")
