// Package control is the single transport-agnostic management contract every
// front-end (http, ubus, cli) drives: the Plane (request/response methods + a
// topic subscription), the Supervisor it drives, and the Diagnostics probe set
// (§7).
//
// Ring: CORE (stdlib + core/bus + core/config). No net/http, no transport types.
// Real types land in step B10.
package control
