package atalk

import (
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/service/zip"
)

// zip.go is the client-side ZIP zone-list query over the ATP requester: it walks the
// GetZoneList / GetLocalZones / GetMyZone pages a router serves and returns the zone
// names. The server ring has the ZIP responder (core/service/zip); this is the requester
// half, so the csgetzones probe rides the same Endpoint + ATP requester (and verbose
// trace) as the AFP client instead of hand-rolling the ATP transaction and TResp parse.
//
// ZIP GetZoneList (Inside Macintosh: Networking, ch. 8) is ATP-carried: DDP type 3 to
// socket 6. The TReq's four ATP user bytes are [function, 0, startIndex_hi, startIndex_lo]
// — which is exactly a big-endian uint32 (function<<24 | startIndex) in the ATP UserData
// — with a one-segment response bitmap. The router answers with a single-packet TResp
// whose four echoed user bytes are [lastFlag, 0, numZones_hi, numZones_lo] followed by the
// length-prefixed zone names. The client re-requests from the next index until the router
// sets lastFlag (or returns an empty page). GetMyZone returns a single unpaged zone.

// ZoneQuery selects which ZIP zone-list function GetZoneList issues.
type ZoneQuery uint8

const (
	// AllZones lists every zone the router knows (GetZoneList).
	AllZones ZoneQuery = iota
	// LocalZones lists only the zones on the requester's own network (GetLocalZones).
	LocalZones
	// MyZone returns just the responding router's own zone (GetMyZone), unpaged.
	MyZone
)

// zipFunc maps a ZoneQuery to the ZIP ATP function byte the responder decodes.
func (q ZoneQuery) zipFunc() byte {
	switch q {
	case LocalZones:
		return zip.ATPGetLocalZoneList
	case MyZone:
		return zip.ATPGetMyZone
	default:
		return zip.ATPGetZoneList
	}
}

// zipLastFlag reports whether a ZIP TResp UserData marks the last page. The responder
// packs the user bytes as [lastFlag, 0, numZones_hi, numZones_lo]; the last flag is the
// high byte.
func zipLastFlag(userData uint32) bool { return userData>>24 != 0 }

// GetZoneList queries a router (dst — use a broadcast node 0xFF to reach any router) for
// its zones and returns them in order. It pages GetZoneList/GetLocalZones by re-requesting
// from the next 1-relative index until the router signals the last page (or returns an
// empty one); GetMyZone is a single request. Each page is one ATP transaction via the
// shared requester (ALO, no TRel — ZIP is not exactly-once), so the verbose trace shows
// the same TReq/TResp narration as an AFP connect.
func (a *ATP) GetZoneList(dst Addr, query ZoneQuery, timeout time.Duration) ([]string, error) {
	dst.Socket = zip.SAS
	fn := query.zipFunc()

	// A zone-list probe wants a bounded wait, not the session-oriented 6×2s retry the ASP
	// callers rely on: one attempt of the caller's timeout per page. Restore the default
	// after so a shared requester (were one reused) is not left with the probe policy.
	if timeout > 0 {
		a.SetRetryPolicy(timeout, 1)
		defer a.SetRetryPolicy(0, 0)
	}

	var zones []string
	startIndex := uint32(1) // ZIP indexes the zone list 1-relative
	for {
		// UserData = [function, 0, startIndex_hi, startIndex_lo]; one response segment,
		// ALO (no exactly-once) — the ZIP responder rejects any bitmap but 1.
		userData := uint32(fn)<<24 | (startIndex & 0xFFFF)
		resp, err := a.Request(dst, userData, nil, false, 1)
		if err != nil {
			return zones, err
		}
		page := parseZoneNames(resp.Data)
		zones = append(zones, page...)
		// GetMyZone is unpaged; otherwise stop when the router flags the last page or
		// returns nothing more to page through.
		if query == MyZone || zipLastFlag(resp.UserData) || len(page) == 0 {
			return zones, nil
		}
		startIndex += uint32(len(page))
	}
}

// parseZoneNames decodes the length-prefixed zone-name list in a GetZoneList response
// page (each entry is a 1-byte length followed by that many name bytes). Empty entries
// are skipped so a padding zero never yields a blank zone.
func parseZoneNames(b []byte) []string {
	var zones []string
	for len(b) >= 1 {
		l := int(b[0])
		if len(b) < 1+l {
			break
		}
		if l > 0 {
			zones = append(zones, string(b[1:1+l]))
		}
		b = b[1+l:]
	}
	return zones
}
