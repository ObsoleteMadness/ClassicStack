// Package http is the HTTP/JSON + SSE control front-end adapter over
// core/control.Plane (§7). It is the browser-facing encoding of the one control
// contract: request/response methods become JSON endpoints, and the telemetry
// Subscribe becomes an SSE stream.
//
// Ring: ADAPTER. When implemented it will import net/http + encoding/json (both
// forbidden in core — reflection-based JSON is an adapter concern, §6d/§13).
//
// Phase 1 status: SEAM ONLY (the user's "very thin — no implementation yet"
// steering). This package fixes the contract the front-end satisfies — the same
// Client interface the in-process and ubus front-ends expose, so the
// multi-front-end parity test (E3) can drive all three uniformly. The concrete
// net/http server + client land when E3/D6 actually exercises them, so we don't
// bake a section/event wire format in before the config/event model settles.
package http

import "github.com/ObsoleteMadness/ClassicStack/adapter/control/inproc"

// Client is the front-end contract this HTTP adapter will satisfy. It is the
// single front-end-agnostic surface (defined once in adapter/control/inproc) that
// inproc, http, and ubus all implement; referencing it here records the seam the
// net/http Server+Client will fill in a later step.
type Client = inproc.Client
