// Package rtmp implements the Routing Table Maintenance Protocol.
//
// It provides a RespondingService (replies to Route Data Requests on
// socket 1) and a SendingService (periodically broadcasts the local
// routing table to neighbouring routers).
//
// See spec/05-rtmp.md and Inside AppleTalk 2/e §5.
package rtmp
