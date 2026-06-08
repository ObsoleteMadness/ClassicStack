package archtest

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// corePrefix is the import path prefix of the constrained ring.
const corePrefix = "github.com/ObsoleteMadness/ClassicStack/core/"

// forbidden lists import paths (exact match) that no core/ runtime package may
// pull in, transitively, plus a comment on why. This IS §1 made executable and
// the no-reflection rule. Adding to this allowlist (i.e. removing an entry)
// requires a comment and a reviewer — do not silently exempt a package.
var forbidden = map[string]string{
	"net/http":                   "control front-ends are adapters, not core",
	"reflect":                    "no-reflection rule (TinyGo + allocation discipline)",
	"encoding/json":              "JSON is an adapter concern (config/control codecs)",
	"log/slog":                   "core/log is the logging contract; slog is an adapter sink",
	"database/sql":               "sqlite/SQL metastore is an adapter",
	"encoding/binary":            "transitively imports reflect; hand-roll big-endian in core (see core/protocol/ddp)",
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
		if ee, ok := err.(*exec.ExitError); ok {
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
