// Package router implements the ClassicStack AppleTalk Phase 2 router core.
//
// The router maintains the routing table (RTMP) and zone information
// table (ZIP), receives DDP datagrams from every registered Port, and
// dispatches them to local Services by socket number or forwards them
// to other ports.
//
// See spec/00-overview.md for socket assignments and the contracts the
// router expects from Service and Port implementations.
package router
