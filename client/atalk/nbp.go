package atalk

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/protocol/nbp"
)

// nbp.go is the client-side NBP name lookup over an Endpoint: it broadcasts a BrRq for
// an entity name and collects the LkUp-Rply tuples, so an AFP client can resolve a
// server's NBP name ("MyMac:MyZone" of type "AFPServer") to a net.node.socket address.

// AFPServerType is the NBP type an AppleShare/AFP server registers under.
const AFPServerType = "AFPServer"

// nbpBroadcastNode is the AppleTalk broadcast node id (0xFF): a BrRq is addressed here
// so the local router forwards it as LkUps into every zone segment.
const nbpBroadcastNode uint8 = 0xFF

// nbpLookupTimeout bounds how long Lookup waits for replies before returning what it
// has (NBP has no "no more replies" signal — the requester waits a fixed window).
const nbpLookupTimeout = 2 * time.Second

// ErrNoNBPMatch is returned by LookupOne when no responder matched the name.
var ErrNoNBPMatch = errors.New("atalk: no NBP responder matched")

// NBPEntity is one resolved NBP tuple: the object/type/zone strings and the address it
// lives at.
type NBPEntity struct {
	Object string
	Type   string
	Zone   string
	Addr   Addr
}

// Lookup broadcasts a BrRq for object:type in zone and returns every LkUp-Rply tuple
// received within the default lookup window. An empty object or type is the '=' wildcard;
// an empty zone is the '*' (this zone) wildcard. Prefer LookupAllZones for server
// discovery — '*' resolves to the requester's local zone only (spec/02-nbp.md).
func (e *Endpoint) Lookup(object, typ, zone string) ([]NBPEntity, error) {
	return e.LookupTimeout(object, typ, zone, nbpLookupTimeout)
}

// LookupAllZones discovers object:type in every zone the internetwork knows. It pages
// ZIP GetZoneList, broadcasts a BrRq into each zone, and collects replies for one window.
// When no router answers the zone list it falls back to Lookup in the local zone ("*").
func (e *Endpoint) LookupAllZones(object, typ string, window time.Duration) ([]NBPEntity, error) {
	if window <= 0 {
		window = nbpLookupTimeout
	}
	zones := filterScanZones(e.fetchAllZones())
	if len(zones) == 0 {
		return e.LookupTimeout(object, typ, "*", window)
	}
	return e.lookupZonesTimeout(object, typ, zones, window)
}

// LookupTimeout is Lookup with a caller-chosen collection window, so a probe (csnbp) can
// honour its -timeout while the default Lookup keeps the fixed discovery window. NBP has
// no "no more replies" signal, so the requester always waits the whole window before
// returning what it collected; a timeout of 0 falls back to the default.
func (e *Endpoint) LookupTimeout(object, typ, zone string, window time.Duration) ([]NBPEntity, error) {
	return e.lookupZonesTimeout(object, typ, []string{zone}, window)
}

// fetchAllZones asks a router (broadcast node 0xFF) for the internetwork zone list via
// ZIP GetZoneList. An empty slice means no router answered — callers fall back to a
// local-zone lookup.
func (e *Endpoint) fetchAllZones() []string {
	local := e.LocalAddr()
	zones, err := NewATP(e).GetZoneList(
		Addr{Network: local.Network, Node: nbpBroadcastNode},
		AllZones,
		nbpLookupTimeout,
	)
	if err != nil {
		tracef("ZIP GetZoneList failed: %v — falling back to local zone", err)
		return nil
	}
	if len(zones) == 0 {
		tracef("ZIP GetZoneList returned no zones — falling back to local zone")
	}
	return zones
}

// lookupZonesTimeout broadcasts BrRq for object:type into each zone and collects LkUp-Rply
// tuples for window. An empty zone entry is the '*' (this zone) wildcard.
func (e *Endpoint) lookupZonesTimeout(object, typ string, zones []string, window time.Duration) ([]NBPEntity, error) {
	if window <= 0 {
		window = nbpLookupTimeout
	}
	obj := wildcardBytes(object, nbp.NameWildcard)
	tp := wildcardBytes(typ, nbp.NameWildcard)

	ch := e.BindSocket(NamesInfoSocket)
	defer e.Unbind(NamesInfoSocket)

	local := e.LocalAddr()
	dst := Addr{Network: local.Network, Node: nbpBroadcastNode, Socket: NamesInfoSocket}
	id := nbpID()
	for _, zone := range zones {
		zn := wildcardBytes(zone, nbp.ZoneWildcard)
		tracef("NBP BrRq %q:%q@%q from %s → broadcast", string(obj), string(tp), string(zn), local)
		pkt := nbp.BuildLkUp(nbp.CtrlBrRq, id, local.Network, local.Node, NamesInfoSocket, obj, tp, zn)
		if err := e.Send(dst, NamesInfoSocket, nbp.DDPType, pkt); err != nil {
			return nil, err
		}
	}

	seen := map[string]NBPEntity{}
	deadline := time.After(window)
	for {
		select {
		case d, ok := <-ch:
			if !ok {
				return nbpEntities(seen), nil
			}
			p, err := nbp.ParsePacket(d.Data)
			if err != nil || p.Function != nbp.CtrlLkUpRply {
				continue
			}
			ent := NBPEntity{
				Object: string(p.Tuple.Object),
				Type:   string(p.Tuple.Type),
				Zone:   string(p.Tuple.Zone),
				Addr: Addr{
					Network: p.Tuple.Network,
					Node:    p.Tuple.Node,
					Socket:  p.Tuple.Socket,
				},
			}
			mergeNBPEntity(seen, ent)
		case <-deadline:
			return nbpEntities(seen), nil
		}
	}
}

// LookupOne resolves a single AFP server by name (object[:zone], type "AFPServer") to
// its address. A ':' in name separates object and zone; without one every zone in the
// internetwork is searched (ZIP GetZoneList + per-zone BrRq). It returns the first
// matching responder, or ErrNoNBPMatch.
func (e *Endpoint) LookupOne(name string) (NBPEntity, error) {
	object, zone := splitNameZone(name)
	var (
		ents []NBPEntity
		err  error
	)
	if zone == "" || zone == "*" {
		ents, err = e.LookupAllZones(object, AFPServerType, nbpLookupTimeout)
	} else {
		ents, err = e.Lookup(object, AFPServerType, zone)
	}
	if err != nil {
		return NBPEntity{}, err
	}
	for _, ent := range ents {
		if strings.EqualFold(ent.Object, object) {
			return ent, nil
		}
	}
	if len(ents) > 0 {
		return ents[0], nil
	}
	return NBPEntity{}, ErrNoNBPMatch
}

// splitNameZone splits an NBP "object:zone" into its parts; a missing zone is "*".
func splitNameZone(name string) (object, zone string) {
	if i := strings.LastIndexByte(name, ':'); i >= 0 {
		return name[:i], name[i+1:]
	}
	return name, "*"
}

// wildcardBytes returns the name bytes, or the single wildcard byte when empty.
func wildcardBytes(s string, wildcard byte) []byte {
	if s == "" || s == "*" || s == "=" {
		return []byte{wildcard}
	}
	return []byte(s)
}

// nbpID returns a per-lookup NBP id byte. It need not be globally unique — only
// distinct enough that a stale reply from a prior lookup is unlikely to be confused;
// the low byte of the current time is sufficient for a one-shot client.
func nbpID() byte { return byte(time.Now().UnixNano()) }

// filterScanZones drops blank and "*" entries and case-insensitive duplicates from a
// ZIP zone list. A BrRq with zone "*" is the local-zone wildcard (spec/02-nbp.md), not
// an all-zones probe — it must not be mixed with explicit per-zone BrRqs.
func filterScanZones(zones []string) []string {
	out := make([]string, 0, len(zones))
	seen := map[string]bool{}
	for _, z := range zones {
		z = strings.TrimSpace(z)
		if nbpWildcardZone(z) {
			continue
		}
		fold := strings.ToLower(z)
		if seen[fold] {
			continue
		}
		seen[fold] = true
		out = append(out, z)
	}
	return out
}

func nbpWildcardZone(z string) bool {
	z = strings.TrimSpace(z)
	return z == "" || z == "*"
}

// nbpDedupKey identifies one NBP responder regardless of whether the reply tuple
// carried a named zone or the "*" local-zone wildcard.
func nbpDedupKey(ent NBPEntity) string {
	return fmt.Sprintf("%d.%d:%s", ent.Addr.Network, ent.Addr.Node, strings.ToLower(ent.Object))
}

// mergeNBPEntity keeps one tuple per responder address, preferring a named zone over "*".
func mergeNBPEntity(seen map[string]NBPEntity, ent NBPEntity) {
	key := nbpDedupKey(ent)
	prev, ok := seen[key]
	if !ok {
		seen[key] = ent
		return
	}
	if nbpWildcardZone(prev.Zone) && !nbpWildcardZone(ent.Zone) {
		seen[key] = ent
	}
}

func nbpEntities(seen map[string]NBPEntity) []NBPEntity {
	out := make([]NBPEntity, 0, len(seen))
	for _, ent := range seen {
		out = append(out, ent)
	}
	return out
}
