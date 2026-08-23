// Package describe builds management SectionInfo from the config schema registry.
// It lives in the ADAPTER ring so it may use reflection — core/config stays
// reflection-free and only carries the FieldInfo / SectionInfo DTOs.
package describe

import (
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/port"
)

// Capability detectors: a section that implements the matching provider advertises
// the capability even when Register omitted Capabilities.
var capabilityChecks = []struct {
	name string
	ok   func(any) bool
}{
	{config.CapCapture, func(s any) bool { _, ok := s.(port.CaptureProvider); return ok }},
	{config.CapSeed, func(s any) bool { _, ok := s.(port.SeedProvider); return ok }},
	{config.CapIPXNetwork, func(s any) bool { _, ok := s.(port.IPXNetworkProvider); return ok }},
	{config.CapWireBinding, func(s any) bool { _, ok := s.(config.InterfaceProvider); return ok }},
}

// All returns one SectionInfo per registered schema, sorted by key. Fields are
// taken from schema.Fields when set, else reflected from schema.New()'s type
// using display/desc/example/default/widget struct tags. Capabilities merge the
// registered list with those detected via type assertion on a fresh New().
func All() []config.SectionInfo {
	schemas := config.Schemas()
	out := make([]config.SectionInfo, 0, len(schemas))
	for _, sc := range schemas {
		out = append(out, Describe(sc))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Describe builds SectionInfo for one schema.
func Describe(sc config.SectionSchema) config.SectionInfo {
	info := config.SectionInfo{
		Key:         sc.Key,
		Repeated:    sc.Repeated,
		DisplayName: sc.DisplayName,
		Description: sc.Description,
	}
	if info.DisplayName == "" {
		info.DisplayName = sc.Key
	}
	caps := append([]string(nil), sc.Capabilities...)
	var sample any
	if sc.New != nil {
		sample = sc.New()
	}
	if sample != nil {
		for _, c := range capabilityChecks {
			if c.ok(sample) && !contains(caps, c.name) {
				caps = append(caps, c.name)
			}
		}
		// Framing / serial / pace are field-tag driven (no dedicated provider), so
		// detect them from reflected field capabilities.
	}
	if len(sc.Fields) > 0 {
		info.Fields = append([]config.FieldInfo(nil), sc.Fields...)
	} else if sample != nil {
		info.Fields = FieldsOf(sample)
	}
	for _, f := range info.Fields {
		if f.Capability != "" && !contains(caps, f.Capability) {
			caps = append(caps, f.Capability)
		}
	}
	sort.Strings(caps)
	info.Capabilities = caps
	return info
}

// FieldsOf reflects exported fields on sec (pointer or value), including anonymous
// embeds, into FieldInfo. Tag vocabulary (all optional):
//
//	display:"Station MAC"     → DisplayName
//	desc:"…"                  → Description
//	example:"00:11:22:…"      → Example
//	default:"0"               → Default
//	widget:"iface"            → Widget
//	capability:"capture"      → Capability
//	secret:"true"             → Secret
//
// toml:"name,omitempty" supplies TOML; the Go field name is Key. toml:"-" skips.
func FieldsOf(sec any) []config.FieldInfo {
	v := reflect.ValueOf(sec)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	var out []config.FieldInfo
	walkFields(v, "", &out)
	return out
}

func walkFields(v reflect.Value, embedCap string, out *[]config.FieldInfo) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" && !sf.Anonymous { // unexported
			continue
		}
		fv := v.Field(i)
		tomlTag := sf.Tag.Get("toml")
		if tomlTag == "-" {
			continue
		}
		// Anonymous embed: recurse; inherit capability from the embed type name when set.
		if sf.Anonymous && fv.Kind() == reflect.Struct && (tomlTag == "" || strings.Split(tomlTag, ",")[0] == "") {
			var cap string
			if c := sf.Tag.Get("capability"); c != "" {
				cap = c
			} else {
				cap = capabilityForEmbed(sf.Type.Name())
			}
			walkFields(fv, cap, out)
			continue
		}
		key := strings.Split(tomlTag, ",")[0]
		if key == "" {
			key = strings.ToLower(sf.Name)
		}
		fi := config.FieldInfo{
			Key:         sf.Name,
			TOML:        key,
			DisplayName: tagOr(sf, "display", humanise(sf.Name)),
			Description: sf.Tag.Get("desc"),
			Example:     sf.Tag.Get("example"),
			Default:     sf.Tag.Get("default"),
			Widget:      sf.Tag.Get("widget"),
			Capability:  firstNonEmpty(sf.Tag.Get("capability"), embedCap),
			Type:        fieldType(sf.Type),
			Secret:      sf.Tag.Get("secret") == "true",
		}
		*out = append(*out, fi)
	}
}

func capabilityForEmbed(typeName string) string {
	switch typeName {
	case "CaptureFields":
		return config.CapCapture
	case "SeedFields":
		return config.CapSeed
	case "SerialFields":
		return config.CapSerial
	case "IPXFrameFields":
		return config.CapIPXFraming
	case "IPXNetworkFields":
		return config.CapIPXNetwork
	case "Base":
		return config.CapWireBinding
	}
	return ""
}

func fieldType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "bool"
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "int"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "uint"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String {
			return "strings"
		}
	}
	return "string"
}

func tagOr(sf reflect.StructField, key, fallback string) string {
	if v := sf.Tag.Get(key); v != "" {
		return v
	}
	return fallback
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// knownAcronyms are Go identifier fragments that must stay unbroken in humanise
// (otherwise MAC → "M A C", FSType → "F S Type", DName → "D Name").
var knownAcronyms = []string{
	"MAC", "IPX", "AFP", "SMB", "NCP", "NBP", "DDP", "TCP", "UDP", "NBT",
	"FS", "CNID", "DOS", "UAM", "ASP", "ATP", "DSI", "ZIP", "RTMP",
}

func humanise(name string) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	i := 0
	for i < len(name) {
		matched := false
		for _, ac := range knownAcronyms {
			if strings.HasPrefix(name[i:], ac) {
				// Only treat as an acronym when it ends the name or the next rune
				// is uppercase / digit / end (so "FSType" → "FS"+"Type", not
				// eating into "Type").
				end := i + len(ac)
				if end == len(name) || (name[end] >= 'A' && name[end] <= 'Z') || (name[end] >= '0' && name[end] <= '9') {
					if b.Len() > 0 {
						b.WriteByte(' ')
					}
					b.WriteString(ac)
					i = end
					matched = true
					break
				}
			}
		}
		if matched {
			continue
		}
		r := rune(name[i])
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteByte(name[i])
		i++
	}
	return b.String()
}

// DefaultValue parses FieldInfo.Default into a Go value suitable for a blank
// instance, using Type as the coercion hint.
func DefaultValue(f config.FieldInfo) any {
	switch f.Type {
	case "bool":
		return f.Default == "true" || f.Default == "1"
	case "int":
		n, _ := strconv.ParseInt(f.Default, 0, 64)
		return int(n)
	case "uint":
		n, _ := strconv.ParseUint(f.Default, 0, 64)
		return uint64(n)
	case "strings":
		if f.Default == "" {
			return []string{}
		}
		return strings.Split(f.Default, ",")
	default:
		return f.Default
	}
}
