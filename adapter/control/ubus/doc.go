// Package ubus is the OpenWRT ubus control front-end adapter: registers a
// classicstack object on ubus.sock, mapping each Plane method to a ubus method
// and Subscribe(topic...) to ubus notifications (§7).
//
// Ring: ADAPTER. Real impl lands in step D6.
package ubus
