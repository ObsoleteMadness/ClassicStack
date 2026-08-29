package archtest

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// corePrefix is the import path prefix of the constrained ring.
const corePrefix = "github.com/ObsoleteMadness/ClassicStack/core/"

// forbidden lists import paths (exact match) that no core/ runtime package may
// pull in, transitively, plus a comment on why. This IS §1 made executable.
//
// Policy: TinyGo's reflect package itself works fine (verified: crypto/rand,
// which uses it, builds and links under real TinyGo — see core/csnet/random.go).
// What TinyGo does NOT reliably support is *generic reflection-based
// serialization* — walking arbitrary structs via struct tags to encode/decode
// them (encoding/json, encoding/binary's Read/Write, database/sql's row
// scanning). So bare "reflect" is not banned; the specific serialization
// packages are, by name, below. Adding to this list (i.e. removing an entry)
// requires a comment and a reviewer — do not silently exempt a package.
//
// Note: fmt is deliberately NOT in this list even though many core/ doc
// comments say "core bans fmt" — crypto/rand itself transitively imports fmt,
// so banning it here would make crypto/rand (and anything importing it, like
// core/csnet.RandomMAC) fail this gate. fmt is still avoided by convention
// (Sprintf/Printf's verb dispatch reflects over its arguments on every call,
// unlike crypto/rand's incidental/unused reflect dependency) — existing
// hand-rolled formatting (core/binaryprimitives, core/router/routing_table.go,
// etc.) should stay hand-rolled — but it is no longer mechanically enforced.
var forbidden = map[string]string{
	"net/http":                   "control front-ends are adapters, not core",
	"encoding/json":              "generic reflect-based (de)serialization TinyGo doesn't reliably support; also an adapter concern (config/control codecs)",
	"log/slog":                   "core/log is the logging contract; slog is an adapter sink",
	"database/sql":               "row-scanning is generic reflect-based (de)serialization TinyGo doesn't reliably support; sqlite/SQL metastore is an adapter",
	"encoding/binary":            "Read/Write are generic reflect-based struct (de)serialization TinyGo doesn't reliably support; hand-roll big-endian in core (see core/binaryprimitives)",
	"github.com/google/gopacket": "capture/link backends are adapters",
	"github.com/knadh/koanf/v2":  "config codecs (koanf/toml) are adapters",
	"modernc.org/sqlite":         "sqlite metastore is an adapter",
}

// forbiddenPrefixes catches families of packages by import-path prefix (e.g.
// every koanf or gopacket subpackage, every pcap binding).
var forbiddenPrefixes = map[string]string{
	"github.com/knadh/koanf":     "config codecs (koanf/toml) are adapters",
	"github.com/google/gopacket": "capture/link backends are adapters",
	"modernc.org/sqlite":         "sqlite metastore is an adapter",
}

// goListPkg is the subset of `go list -json` output we consume.
type goListPkg struct {
	ImportPath string
	Deps       []string // full transitive import set
}

// TestCoreImportGraph walks every core/... package's transitive imports and
// fails if any forbidden package is reachable. It shells out to `go list`
// (stdlib + os/exec only) so the test itself adds no heavy build-time dep.
func TestCoreImportGraph(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-json", "github.com/ObsoleteMadness/ClassicStack/core/...")
	out, err := cmd.Output()
	if err != nil {
		ee := &exec.ExitError{}
		if errors.As(err, &ee) {
			t.Fatalf("go list failed: %v\nstderr:\n%s", err, ee.Stderr)
		}
		t.Fatalf("go list failed: %v", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))
	var violations []string
	for dec.More() {
		var pkg goListPkg
		if err := dec.Decode(&pkg); err != nil {
			t.Fatalf("decoding go list output: %v", err)
		}
		// Only constrain packages that ARE in the core ring (go list -deps
		// also emits their dependencies, which we must not constrain).
		if !strings.HasPrefix(pkg.ImportPath, corePrefix) {
			continue
		}
		for _, dep := range pkg.Deps {
			if why, bad := forbidden[dep]; bad {
				violations = append(violations,
					pkg.ImportPath+" imports "+dep+" ("+why+")")
				continue
			}
			for pfx, why := range forbiddenPrefixes {
				if dep == pfx || strings.HasPrefix(dep, pfx+"/") {
					violations = append(violations,
						pkg.ImportPath+" imports "+dep+" ("+why+")")
				}
			}
		}
	}

	if len(violations) > 0 {
		t.Fatalf("core/ dependency rule violated (§1):\n  %s",
			strings.Join(violations, "\n  "))
	}
}
