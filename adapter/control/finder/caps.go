package finder

import (
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/fs"
)

const (
	AddressCNID = "cnid"
	AddressPath = "path"
)

// CatalogCapabilities is the JSON object on SessionInfo: identity (chrome) plus
// feature schema. FinderWindow branches on features, never on shareKind.
type CatalogCapabilities struct {
	Identity      VolumeIdentity `json:"identity"`
	AddressBy     string         `json:"addressBy"`
	ReadOnly      bool           `json:"readOnly"`
	ResourceFork  bool           `json:"resourceFork"`
	FinderInfo    bool           `json:"finderInfo"`
	DesktopIcons  bool           `json:"desktopIcons"`
	ResourceIcons bool           `json:"resourceIcons"`
	Names         []string       `json:"names"`
	MaxNameBytes  map[string]int `json:"maxNameBytes"`
	NameCase      string         `json:"nameCase"`
	Dates         []string       `json:"dates"`
	Attributes    []AttrField    `json:"attributes"`
	HideAttribute string         `json:"hideAttribute,omitempty"`
	PathFormat    string         `json:"pathFormat"`
}

// VolumeIdentity is chrome-only (volume glyph, path formatting).
type VolumeIdentity struct {
	ShareKind   string `json:"shareKind"`
	Protocol    string `json:"protocol,omitempty"`
	Filesystem  string `json:"filesystem,omitempty"`
	Transport   string `json:"transport,omitempty"`
	ForkBackend string `json:"forkBackend,omitempty"`
	Dialect     string `json:"dialect,omitempty"`
	OS          string `json:"os,omitempty"`
}

// AttrField is one boolean file-flag checkbox in Get Info.
type AttrField struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Editable bool   `json:"editable,omitempty"`
}

var (
	dosAttrs = []AttrField{
		{ID: "readonly", Label: "Read only", Type: "bool", Editable: true},
		{ID: "hidden", Label: "Hidden", Type: "bool", Editable: true},
		{ID: "system", Label: "System", Type: "bool", Editable: true},
		{ID: "archive", Label: "Archive", Type: "bool", Editable: true},
	}
	afpAttrs = []AttrField{
		{ID: "invisible", Label: "Invisible", Type: "bool", Editable: true},
		{ID: "locked", Label: "Locked", Type: "bool", Editable: true},
	}
	afpDates = []string{"created", "modified", "backup"}
	dosDates = []string{"created", "modified", "accessed"}
)

func (sess *Session) protocol() string {
	if sess.Protocol != "" {
		return sess.Protocol
	}
	if sess.Kind == KindLocal {
		return KindAFP
	}
	return sess.Kind
}

func (sess *Session) addressBy() string {
	switch sess.protocol() {
	case KindSMB, KindNCP, KindEtherDFS:
		return AddressPath
	default:
		return AddressCNID
	}
}

func forkCapsOf(ffs fs.ForkFS) fs.ForkCapability {
	if f, ok := ffs.(fs.ForkFeatures); ok {
		return f.ForkCapabilities()
	}
	return fs.ForkCapability{ResourceFork: true, FinderInfo: true, Comment: true}
}

func forkBackendName(ffs fs.ForkFS) string {
	if n, ok := ffs.(fs.ForkEngineNamer); ok {
		if name := strings.TrimSpace(n.ForkEngineName()); name != "" {
			return name
		}
	}
	c := forkCapsOf(ffs)
	if !c.ResourceFork && !c.FinderInfo {
		return "nofork"
	}
	return "appledouble"
}

func (sess *Session) capabilities() CatalogCapabilities {
	proto := sess.protocol()
	caps := presetCaps(proto)
	caps.Identity.ShareKind = sess.Kind
	if sess.Kind == KindLocal {
		caps.Identity.Protocol = proto
		caps.Identity.Filesystem = "local_fs"
		caps = localUnion(caps)
	} else {
		caps.Identity.Protocol = proto
	}
	caps.Identity.Transport = sess.transport
	caps.Identity.Dialect = sess.dialect
	caps.Identity.OS = sess.os
	caps.ReadOnly = sess.readOnly
	caps.AddressBy = sess.addressBy()

	if sess.FS != nil {
		caps.Identity.ForkBackend = forkBackendName(sess.FS)
		fc := forkCapsOf(sess.FS)
		if view, ok := volumeViewOf(sess.FS); ok {
			applyVolumeView(&caps, view)
		} else if sess.Kind == KindLocal {
			caps = localUnion(caps)
			caps.AddressBy = sess.addressBy()
		}
		applyForkCaps(&caps, fc)
		if sess.FS.Capabilities().ReadOnly {
			caps.ReadOnly = true
		}
	}
	return caps
}

// applyForkCaps intersects the protocol preset with the fork adapter. AppleDouble
// on SMB/NCP/EtherDFS does not promote FinderInfo or resourceFork: those protocols
// do not store Macintosh catalog fields. nofork turns the flags off on AFP too.
func applyForkCaps(caps *CatalogCapabilities, fc fs.ForkCapability) {
	caps.ResourceFork = caps.ResourceFork && fc.ResourceFork
	caps.FinderInfo = caps.FinderInfo && fc.FinderInfo
	caps.ResourceIcons = caps.ResourceFork
}

func volumeViewOf(ffs fs.ForkFS) (fs.VolumeViewInfo, bool) {
	if v, ok := ffs.(interface {
		CatalogView() (fs.VolumeViewInfo, bool)
	}); ok {
		return v.CatalogView()
	}
	if v, ok := ffs.(fs.VolumeView); ok {
		return v.CatalogView(), true
	}
	return fs.VolumeViewInfo{}, false
}

func applyVolumeView(caps *CatalogCapabilities, view fs.VolumeViewInfo) {
	if len(view.Names) > 0 {
		caps.Names = view.Names
	}
	if len(view.Dates) > 0 {
		caps.Dates = view.Dates
	}
	if view.NameCase != "" {
		caps.NameCase = view.NameCase
	}
	if view.PathFormat != "" {
		caps.PathFormat = view.PathFormat
	}
	if view.HideAttribute != "" {
		caps.HideAttribute = view.HideAttribute
	}
	if len(view.MaxNameBytes) > 0 {
		caps.MaxNameBytes = view.MaxNameBytes
	}
	if len(view.Attributes) > 0 {
		caps.Attributes = attrsFromIDs(view.Attributes)
	}
}

func attrsFromIDs(ids []string) []AttrField {
	byID := map[string]AttrField{}
	for _, a := range append(append([]AttrField{}, afpAttrs...), dosAttrs...) {
		byID[a.ID] = a
	}
	out := make([]AttrField, 0, len(ids))
	for _, id := range ids {
		if a, ok := byID[id]; ok {
			out = append(out, a)
		} else {
			out = append(out, AttrField{ID: id, Label: id, Type: "bool", Editable: true})
		}
	}
	return out
}

func localUnion(base CatalogCapabilities) CatalogCapabilities {
	base.Names = []string{"long", "medium", "short"}
	base.MaxNameBytes = map[string]int{"long": 255, "medium": 31, "short": 12}
	base.NameCase = "preserve"
	base.Dates = []string{"created", "modified", "accessed"}
	base.Attributes = append(append([]AttrField{}, afpAttrs...), dosAttrs...)
	base.HideAttribute = "invisible"
	base.PathFormat = "posix"
	base.DesktopIcons = false
	return base
}

func presetCaps(proto string) CatalogCapabilities {
	switch proto {
	case KindSMB:
		return CatalogCapabilities{
			Identity:      VolumeIdentity{ShareKind: KindSMB, Protocol: KindSMB},
			AddressBy:     AddressPath,
			Names:         []string{"long", "short"},
			MaxNameBytes:  map[string]int{"long": 255, "short": 12},
			NameCase:      "preserve",
			Dates:         dosDates,
			Attributes:    dosAttrs,
			HideAttribute: "hidden",
			PathFormat:    "dos",
		}
	case KindNCP:
		return CatalogCapabilities{
			Identity:      VolumeIdentity{ShareKind: KindNCP, Protocol: KindNCP},
			AddressBy:     AddressPath,
			Names:         []string{"long", "short"},
			MaxNameBytes:  map[string]int{"long": 255, "short": 12},
			NameCase:      "insensitive",
			Dates:         dosDates,
			Attributes:    dosAttrs,
			HideAttribute: "hidden",
			PathFormat:    "ncp",
		}
	case KindEtherDFS:
		return CatalogCapabilities{
			Identity:      VolumeIdentity{ShareKind: KindEtherDFS, Protocol: KindEtherDFS},
			AddressBy:     AddressPath,
			Names:         []string{"short"},
			MaxNameBytes:  map[string]int{"short": 12},
			NameCase:      "upper",
			Dates:         dosDates,
			Attributes:    dosAttrs,
			HideAttribute: "hidden",
			PathFormat:    "dos",
		}
	default:
		return CatalogCapabilities{
			Identity:      VolumeIdentity{ShareKind: KindAFP, Protocol: KindAFP},
			AddressBy:     AddressCNID,
			ResourceFork:  true,
			FinderInfo:    true,
			DesktopIcons:  true,
			ResourceIcons: true,
			Names:         []string{"long"},
			MaxNameBytes:  map[string]int{"long": 31},
			NameCase:      "preserve",
			Dates:         afpDates,
			Attributes:    afpAttrs,
			HideAttribute: "invisible",
			PathFormat:    "mac",
		}
	}
}
