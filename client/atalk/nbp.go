package atalk

import (
	"errors"
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
// received within the lookup window. An empty object or type is the '=' wildcard; an
// empty zone is the '*' (this zone) wildcard. This is the discovery primitive
// `csfs discover afp` uses.
func (e *Endpoint) Lookup(object, typ, zone string) ([]NBPEntity, error) {
	obj := wildcardBytes(object, nbp.NameWildcard)
	tp := wildcardBytes(typ, nbp.NameWildcard)
	zn := wildcardBytes(zone, nbp.ZoneWildcard)

	ch := e.BindSocket(NamesInfoSocket)
	defer e.Unbind(NamesInfoSocket)

	local := e.LocalAddr()
	tracef("NBP BrRq %q:%q@%q from %s → broadcast", string(obj), string(tp), string(zn), local)
	pkt := nbp.BuildLkUp(nbp.CtrlBrRq, nbpID(), local.Network, local.Node, NamesInfoSocket, obj, tp, zn)
	dst := Addr{Network: local.Network, Node: nbpBroadcastNode, Socket: NamesInfoSocket}
	if err := e.Send(dst, NamesInfoSocket, nbp.DDPType, pkt); err != nil {
		return nil, err
	}

	var out []NBPEntity
	seen := map[string]bool{}
	deadline := time.After(nbpLookupTimeout)
	for {
		select {
		case d, ok := <-ch:
			if !ok {
				return out, nil
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
			key := ent.Object + ":" + ent.Type + ":" + ent.Zone
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ent)
		case <-deadline:
			return out, nil
		}
	}
}

// LookupOne resolves a single AFP server by name (object[:zone], type "AFPServer") to
// its address. A ':' in name separates object and zone; without one the zone is the
// local zone wildcard. It returns the first matching responder, or ErrNoNBPMatch.
func (e *Endpoint) LookupOne(name string) (NBPEntity, error) {
	object, zone := splitNameZone(name)
	ents, err := e.Lookup(object, AFPServerType, zone)
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
