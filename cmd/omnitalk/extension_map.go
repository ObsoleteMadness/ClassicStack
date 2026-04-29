//go:build afp || all

package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/pgodw/omnitalk/service/afp"
)

var extMapLinePattern = regexp.MustCompile(`^(\S+)\s+"([^"]*)"\s+"([^"]*)"`)

func loadAFPExtensionMap(path string) (*afp.ExtensionMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseAFPExtensionMap(data)
}

func parseAFPExtensionMap(data []byte) (*afp.ExtensionMap, error) {
	entries := make(map[string]afp.ExtensionMapping)
	lines := strings.Split(string(data), "\n")

	for i, rawLine := range lines {
		line := strings.TrimSpace(strings.TrimRight(rawLine, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		match := extMapLinePattern.FindStringSubmatch(line)
		if len(match) != 4 {
			return nil, fmt.Errorf("invalid extension map line %d: %q", i+1, rawLine)
		}

		mapping, err := afp.NewExtensionMapping(match[2], match[3])
		if err != nil {
			return nil, fmt.Errorf("invalid extension map line %d: %w", i+1, err)
		}
		entries[strings.ToLower(match[1])] = mapping
	}

	return afp.NewExtensionMap(entries)
}
