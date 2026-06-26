package cli

import (
	diagadapter "github.com/ObsoleteMadness/ClassicStack/adapter/control/diag"
	composediag "github.com/ObsoleteMadness/ClassicStack/compose/diag"
	"github.com/ObsoleteMadness/ClassicStack/compose/runtime"
)

// buildDiagnostics constructs the NEUTRAL control-plane diagnostics impl (ListZones over
// the runtime's router). The protocol-specific drill-downs are NOT here — they are served
// by the diagnostics adapter (buildDiagProvider) so no protocol type crosses core/control.
func buildDiagnostics(rt *runtime.Runtime) *composediag.Diagnostics {
	return composediag.New(rt.Router())
}

// buildDiagProvider builds the protocol diagnostics adapter over the runtime's component
// set (resolving NBP/MacIP live), for the web/ubus servers to serve the registered-names
// and macip-leases drill-downs.
func buildDiagProvider(rt *runtime.Runtime) *diagadapter.Provider {
	return diagadapter.New(rt)
}
