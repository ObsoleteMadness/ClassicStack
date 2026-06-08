// Package supervisor owns the component dependency DAG: ordered start/stop,
// StateChanged publication, and the addressed (no-diff) Reconfigure-and-notify
// cascade. It implements control.Supervisor (§3/§11).
//
// Ring: COMPOSE. Real types land in steps C2 and C3.
package supervisor
