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

	// Marshal well-known logging section
	if err := c.marshalSection(&buf, "logging", "", m.Logging); err != nil {
		return nil, err
	}
	// Marshal well-known router section
	if err := c.marshalSection(&buf, "router", "", m.Router); err != nil {
		return nil, err
	}
	// Marshal well-known bridge section
	if err := c.marshalSection(&buf, "bridge", "", m.Bridge); err != nil {
		return nil, err
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

	// Marshal component sections
	for _, key := range keys {
		sec := m.Sections[key]
		typeName := strings.ToLower(key)
		if err := c.marshalSection(&buf, typeName, key, sec); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
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

	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("toml")
		if tag == "-" {
			continue
		}
		key := strings.Split(tag, ",")[0]
		if key == "" {
			key = strings.ToLower(field.Name)
		}

		fVal := v.Field(i)
		switch fVal.Kind() {
		case reflect.String:
			buf.WriteString(fmt.Sprintf("\toption %s '%s'\n", key, escapeQuote(fVal.String())))
		case reflect.Bool:
			valStr := "0"
			if fVal.Bool() {
				valStr = "1"
			}
			buf.WriteString(fmt.Sprintf("\toption %s '%s'\n", key, valStr))
		case reflect.Int, reflect.Int64:
			buf.WriteString(fmt.Sprintf("\toption %s '%d'\n", key, fVal.Int()))
		case reflect.Slice:
			if fVal.Type().Elem().Kind() == reflect.String {
				for j := 0; j < fVal.Len(); j++ {
					buf.WriteString(fmt.Sprintf("\tlist %s '%s'\n", key, escapeQuote(fVal.Index(j).String())))
				}
			}
		}
	}
	buf.WriteString("\n")
	return nil
}

// Unmarshal parses UCI text into the model.
func (c *Codec) Unmarshal(data []byte, m *config.Model) error {
	sections, err := parseUCI(data)
	if err != nil {
		return err
	}

	// Reset model sections map
	m.Sections = make(map[string]config.Section)

	for _, sec := range sections {
		switch sec.Type {
		case "logging":
			if err := unmarshalStruct(sec, &m.Logging); err != nil {
				return err
			}
		case "router":
			if err := unmarshalStruct(sec, &m.Router); err != nil {
				return err
			}
		case "bridge":
			if err := unmarshalStruct(sec, &m.Bridge); err != nil {
				return err
			}
		default:
			// Match component sections
			for _, schema := range config.Schemas() {
				if strings.ToLower(schema.Key) == sec.Type || schema.Key == sec.Name {
					typedSec := schema.New()
					if err := unmarshalStruct(sec, typedSec); err != nil {
						return err
					}
					m.Sections[schema.Key] = typedSec
					break
				}
			}
		}
	}

	return nil
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
				quoteChar = r
			} else if r == ' ' || r == '\t' {
				if current.Len() > 0 {
					tokens = append(tokens, current.String())
					current.Reset()
				}
			} else {
				current.WriteRune(r)
			}
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func unmarshalStruct(sec uciSection, dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("dest must be a pointer to a struct")
	}
	val := v.Elem()
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("toml")
		if tag == "-" {
			continue
		}
		key := strings.Split(tag, ",")[0]
		if key == "" {
			key = strings.ToLower(field.Name)
		}

		fVal := val.Field(i)
		if optVal, ok := sec.Options[key]; ok {
			switch fVal.Kind() {
			case reflect.String:
				fVal.SetString(optVal)
			case reflect.Bool:
				fVal.SetBool(optVal == "1" || strings.ToLower(optVal) == "true" || strings.ToLower(optVal) == "on")
			case reflect.Int, reflect.Int64:
				num, _ := strconv.ParseInt(optVal, 10, 64)
				fVal.SetInt(num)
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
