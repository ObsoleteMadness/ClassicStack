package config

// Capability names a reusable config field-group a section may expose. Front-ends
// discover these via the schema API so a new protocol that embeds CaptureFields /
// IPXNetworkFields lights up the matching UI without a dedicated SPA change.
const (
	CapWireBinding = "wire_binding"   // Name/Iface/IsEnabled/MAC (port.Base)
	CapCapture     = "capture"        // Capture / CaptureSnaplen
	CapSeed        = "appletalk_seed" // SeedNetwork / SeedNetworkEnd / SeedZone
	CapSerial      = "serial"         // Device / Baud
	CapIPXNetwork  = "ipx_network"    // IPXNetwork
	CapIPXFraming  = "ipx_framing"    // IPXFrameType / IPXFrameTypes
	CapPace        = "localtalk_pace" // PaceMs
)

// FieldInfo describes one configurable field for a management front-end. It is the
// schema half of a section: DisplayName/Description/Example/Default drive labels and
// placeholders; Type/Widget hint how to render the control; Capability groups the
// field with its peers (so a UI can show a "Capture" subsection).
//
// Key is the JSON / Go exported field name Config() emits (e.g. "IPXNetwork").
// TOML is the on-disk key (e.g. "ipx_network"). Adapters fill FieldInfo by reflecting
// section structs (core stays reflection-free); Register may also supply Fields
// explicitly when reflection cannot see them.
type FieldInfo struct {
	Key         string `json:"key"`
	TOML        string `json:"toml,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	Example     string `json:"example,omitempty"`
	Default     string `json:"default,omitempty"` // string form; UI coerces by Type
	Type        string `json:"type"`              // "string"|"bool"|"int"|"uint"|"strings"
	Widget      string `json:"widget,omitempty"`  // optional hint: "iface"|"serial"|"frame_type"|"zone"|…
	Capability  string `json:"capability,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
}

// SectionInfo is the management view of one registered config section: identity,
// whether it is repeated, human copy, capability flags, and the field schema a UI
// uses to render a generic form. It is what GET /schemas returns per entry.
type SectionInfo struct {
	Key          string      `json:"key"`
	Repeated     bool        `json:"repeated"`
	DisplayName  string      `json:"display_name,omitempty"`
	Description  string      `json:"description,omitempty"`
	Capabilities []string    `json:"capabilities,omitempty"`
	Fields       []FieldInfo `json:"fields,omitempty"`
}
