// Package zip implements the AppleTalk Zone Information Protocol.
//
// It provides a RespondingService (answers ZIP queries on socket 6)
// and a SendingService (issues ZIP queries to discover zones for
// networks added by RTMP).
//
// See spec/06-zip.md and Inside AppleTalk 2/e §8.
package zip
