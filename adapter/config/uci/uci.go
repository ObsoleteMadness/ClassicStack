package uci

import (
	"bufio"
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// Codec marshals/unmarshals a config.Model to/from OpenWRT UCI syntax.
type Codec struct{}

// New returns a new UCI codec.
func New() *Codec { return &Codec{} }

// compile-time assertion: *Codec satisfies config.Codec.
var _ config.Codec = (*Codec)(nil)

type uciSection struct {
	Type    string
	Name    string
	Options map[string]string
	Lists   map[string][]string
}

// Marshal renders the model in UCI format.
func (c *Codec) Marshal(m *config.Model) ([]byte, error) {
	var buf bytes.Buffer

	// Write package declaration
	buf.WriteString("package classicstack\n\n")

	// Marshal well-known identity section (server hostname/workgroup/description, §4-bis)
	if err := c.marshalSection(&buf, "identity", "", m.Identity); err != nil {
		return nil, err
	}
	// Marshal the well-known web-admin credential (§4-ter), only once configured so a
	// fresh config has no empty adminauth block (first-run detection stays clean).
	if m.AdminAuth.Configured() {
		if err := c.marshalSection(&buf, "adminauth", "", m.AdminAuth); err != nil {
			return nil, err
		}
	}
	// Marshal well-known logging section
	if err := c.marshalSection(&buf, "logging", "", m.Logging); err != nil {
		return nil, err
	}
	// Marshal well-known router section
	if err := c.marshalSection(&buf, "router", "", m.Router); err != nil {
		return nil, err
	}
	// The legacy singleton `config bridge` block is no longer emitted (pre-M11):
	// a bridge is now an ordinary namespace entry below (kind=bridge, default=1).

	// Marshal the named interface namespace (§M11): one `config interface '<name>'`
	// block per entry, sorted by name for deterministic output.
	ifaceNames := make([]string, 0, len(m.Interfaces))
	for name := range m.Interfaces {
		ifaceNames = append(ifaceNames, name)
	}
	sortStrings(ifaceNames)
	for _, name := range ifaceNames {
		if err := c.marshalSection(&buf, "interface", name, m.Interfaces[name]); err != nil {
			return nil, err
		}
	}

	// Sort component keys for deterministic marshalling
	keys := make([]string, 0, len(m.Sections))
	for k := range m.Sections {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	// Marshal singleton component sections
	for _, key := range keys {
		sec := m.Sections[key]
		typeName := strings.ToLower(key)
		if err := c.marshalSection(&buf, typeName, key, sec); err != nil {
			return nil, err
		}
	}

	// Marshal repeated (named-instance) sections: one `config <type> '<name>'` block
	// per instance, the natural UCI idiom (e.g. config volume 'public'). Keys are
	// sorted for deterministic output; instances keep their model (document) order.
	listKeys := make([]string, 0, len(m.Lists))
	for k := range m.Lists {
		if len(m.Lists[k]) > 0 {
			listKeys = append(listKeys, k)
		}
	}
	sortStrings(listKeys)
	for _, key := range listKeys {
		typeName := strings.ToLower(key)
		for _, sec := range m.Lists[key] {
			name := ""
			if ns, ok := sec.(config.NamedSection); ok {
				name = ns.InstanceName()
			}
			if err := c.marshalSection(&buf, typeName, name, sec); err != nil {
				return nil, err
			}
		}
	}

	return buf.Bytes(), nil
}

// sortStrings is a tiny in-place string sort (the codec already sorts component keys
// with an inline bubble; centralising it keeps both call sites consistent).
func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func (c *Codec) marshalSection(buf *bytes.Buffer, typeName, name string, sec any) error {
	v := reflect.ValueOf(sec)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil // skip if not struct
	}

	if name != "" {
		buf.WriteString(fmt.Sprintf("config %s '%s'\n", typeName, name))
	} else {
		buf.WriteString(fmt.Sprintf("config %s\n", typeName))
	}

	marshalStructFields(buf, v)
	buf.WriteString("\n")
	return nil
}

// marshalStructFields writes UCI options for every exported field on v, recursing
// into anonymous embedded structs (so port.Base / CaptureFields flatten the same
// way go-toml does). Fields tagged omitempty are skipped when zero.
func marshalStructFields(buf *bytes.Buffer, v reflect.Value) {
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := typ.Field(i)
		fVal := v.Field(i)
		tag := field.Tag.Get("toml")
		if tag == "-" {
			continue
		}
		// Anonymous embedded struct with no toml key of its own: promote fields.
		if field.Anonymous && fVal.Kind() == reflect.Struct && (tag == "" || strings.Split(tag, ",")[0] == "") {
			marshalStructFields(buf, fVal)
			continue
		}
		parts := strings.Split(tag, ",")
		key := parts[0]
		if key == "" {
			key = strings.ToLower(field.Name)
		}
		omitEmpty := false
		for _, p := range parts[1:] {
			if p == "omitempty" {
				omitEmpty = true
				break
			}
		}
		if omitEmpty && fVal.IsZero() {
			continue
		}
		switch fVal.Kind() {
		case reflect.String:
			buf.WriteString(fmt.Sprintf("\toption %s '%s'\n", key, escapeQuote(fVal.String())))
		case reflect.Bool:
			valStr := "0"
			if fVal.Bool() {
				valStr = "1"
			}
			buf.WriteString(fmt.Sprintf("\toption %s '%s'\n", key, valStr))
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			buf.WriteString(fmt.Sprintf("\toption %s '%d'\n", key, fVal.Int()))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			buf.WriteString(fmt.Sprintf("\toption %s '%d'\n", key, fVal.Uint()))
		case reflect.Slice:
			if fVal.Type().Elem().Kind() == reflect.String {
				for j := 0; j < fVal.Len(); j++ {
					buf.WriteString(fmt.Sprintf("\tlist %s '%s'\n", key, escapeQuote(fVal.Index(j).String())))
				}
			}
		}
	}
}

// Unmarshal parses UCI text into the model.
func (c *Codec) Unmarshal(data []byte, m *config.Model) error {
	sections, err := parseUCI(data)
	if err != nil {
		return err
	}

	// Reset model section maps
	m.Sections = make(map[string]config.Section)
	m.Lists = make(map[string][]config.Section)

	// A pre-M11 `config bridge` block is captured here and migrated into the
	// interface namespace AFTER the loop, so a modern [[interface]] of the same name
	// (read in the same pass) takes precedence over the legacy block.
	var legacyBridge config.InterfaceSection

	for _, sec := range sections {
		switch sec.Type {
		case "identity":
			if err := unmarshalStruct(sec, &m.Identity); err != nil {
				return err
			}
		case "adminauth":
			if err := unmarshalStruct(sec, &m.AdminAuth); err != nil {
				return err
			}
		case "logging":
			if err := unmarshalStruct(sec, &m.Logging); err != nil {
				return err
			}
		case "router":
			if err := unmarshalStruct(sec, &m.Router); err != nil {
				return err
			}
		case "bridge":
			// Legacy pre-M11 singleton; captured for migration after the loop.
			if err := unmarshalStruct(sec, &legacyBridge); err != nil {
				return err
			}
			legacyBridge.Name = sec.Name
		case "interface":
			// One named interface-namespace entry (§M11). The UCI block name is the
			// authoritative interface name.
			var iface config.InterfaceSection
			if err := unmarshalStruct(sec, &iface); err != nil {
				return err
			}
			iface.Name = sec.Name
			m.SetInterface(iface)
		default:
			// Match component sections (singleton → Sections; repeated → Lists).
			for _, schema := range config.Schemas() {
				if strings.ToLower(schema.Key) != sec.Type && schema.Key != sec.Name {
					continue
				}
				typedSec := schema.New()
				if err := unmarshalStruct(sec, typedSec); err != nil {
					return err
				}
				if schema.Repeated {
					// The UCI block name is the authoritative instance key, so a
					// NamedSection whose name field went unset (or diverged) is
					// reconciled to the block name here.
					applyInstanceName(typedSec, sec.Name)
					m.Lists[schema.Key] = append(m.Lists[schema.Key], typedSec)
				} else {
					m.Sections[schema.Key] = typedSec
				}
				break
			}
		}
	}

	// Fold a captured pre-M11 bridge into the namespace (no-op when absent or when a
	// modern [[interface]] of that name was read above).
	m.MigrateLegacyBridge(legacyBridge)

	return nil
}

// applyInstanceName reconciles a repeated section's name field with the UCI block
// name when they differ (the block name is authoritative). It re-marshals the block
// name into the field named by the section's NamedSection key via the same struct
// path the option loop uses, so no per-type knowledge leaks into the codec.
func applyInstanceName(sec config.Section, blockName string) {
	if blockName == "" {
		return
	}
	ns, ok := sec.(config.NamedSection)
	if !ok || ns.InstanceName() == blockName {
		return
	}
	v := reflect.ValueOf(sec)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}
	setNameField(v, blockName)
}

// setNameField finds the first string field tagged toml:"name" (including inside
// anonymous embeds) and sets it to blockName.
func setNameField(v reflect.Value, blockName string) bool {
	typ := v.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fVal := v.Field(i)
		tag := field.Tag.Get("toml")
		if field.Anonymous && fVal.Kind() == reflect.Struct && (tag == "" || strings.Split(tag, ",")[0] == "") {
			if setNameField(fVal, blockName) {
				return true
			}
			continue
		}
		if strings.Split(tag, ",")[0] != "name" {
			continue
		}
		if fVal.Kind() == reflect.String && fVal.CanSet() {
			fVal.SetString(blockName)
			return true
		}
		return false
	}
	return false
}

func parseUCI(data []byte) ([]uciSection, error) {
	var sections []uciSection
	var current *uciSection

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "package ") {
			continue
		}

		tokens := tokenize(line)
		if len(tokens) == 0 {
			continue
		}

		switch tokens[0] {
		case "config":
			if len(tokens) < 2 {
				return nil, fmt.Errorf("invalid config line: %s", line)
			}
			secType := tokens[1]
			secName := ""
			if len(tokens) >= 3 {
				secName = tokens[2]
			}
			sections = append(sections, uciSection{
				Type:    secType,
				Name:    secName,
				Options: make(map[string]string),
				Lists:   make(map[string][]string),
			})
			current = &sections[len(sections)-1]

		case "option":
			if current == nil {
				return nil, fmt.Errorf("option outside config section: %s", line)
			}
			if len(tokens) < 3 {
				return nil, fmt.Errorf("invalid option line: %s", line)
			}
			current.Options[tokens[1]] = tokens[2]

		case "list":
			if current == nil {
				return nil, fmt.Errorf("list outside config section: %s", line)
			}
			if len(tokens) < 3 {
				return nil, fmt.Errorf("invalid list line: %s", line)
			}
			key := tokens[1]
			current.Lists[key] = append(current.Lists[key], tokens[2])
		}
	}

	return sections, scanner.Err()
}

func tokenize(line string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	// quoted records that the current token had an opening quote, so an empty
	// quoted value ('' — e.g. an unset string option) emits an empty token
	// rather than being dropped (which would corrupt the option's arity).
	quoted := false
	var quoteChar rune

	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if inQuote {
			if r == quoteChar {
				inQuote = false
			} else {
				current.WriteRune(r)
			}
		} else {
			if r == '\'' || r == '"' {
				inQuote = true
				quoted = true
				quoteChar = r
			} else if r == ' ' || r == '\t' {
				if current.Len() > 0 || quoted {
					tokens = append(tokens, current.String())
					current.Reset()
					quoted = false
				}
			} else {
				current.WriteRune(r)
			}
		}
	}
	if current.Len() > 0 || quoted {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func unmarshalStruct(sec uciSection, dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("dest must be a pointer to a struct")
	}
	return unmarshalStructFields(sec, v.Elem())
}

// unmarshalStructFields fills dest from UCI options/lists, recursing into
// anonymous embedded structs so port.Base / CaptureFields decode like go-toml.
func unmarshalStructFields(sec uciSection, val reflect.Value) error {
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		fVal := val.Field(i)
		tag := field.Tag.Get("toml")
		if tag == "-" {
			continue
		}
		if field.Anonymous && fVal.Kind() == reflect.Struct && (tag == "" || strings.Split(tag, ",")[0] == "") {
			if err := unmarshalStructFields(sec, fVal); err != nil {
				return err
			}
			continue
		}
		key := strings.Split(tag, ",")[0]
		if key == "" {
			key = strings.ToLower(field.Name)
		}

		if optVal, ok := sec.Options[key]; ok {
			switch fVal.Kind() {
			case reflect.String:
				fVal.SetString(optVal)
			case reflect.Bool:
				fVal.SetBool(optVal == "1" || strings.ToLower(optVal) == "true" || strings.ToLower(optVal) == "on")
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				num, _ := strconv.ParseInt(optVal, 10, 64)
				fVal.SetInt(num)
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				num, _ := strconv.ParseUint(optVal, 10, 64)
				fVal.SetUint(num)
			}
		} else if listVals, ok := sec.Lists[key]; ok {
			if fVal.Kind() == reflect.Slice && fVal.Type().Elem().Kind() == reflect.String {
				fVal.Set(reflect.ValueOf(listVals))
			}
		}
	}
	return nil
}

func escapeQuote(s string) string {
	return strings.ReplaceAll(s, "'", `\'`)
}
