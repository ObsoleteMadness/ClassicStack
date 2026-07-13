package fs

import (
	"strconv"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
)

// derivedNameEngine maps long store names to derived short (8.3, for DOS/Windows
// SMB clients) and medium (31-char, for classic AFP) names, persisting the
// binding in the metastore so a derived name keeps mapping back to the same long
// name across restarts. It is the real engine behind the "short"/"medium" share
// NameEngine names, porting pkg/shortname's 8.3 derivation onto the metastore.
type derivedNameEngine struct {
	store metastore.Store
}

// NewDerivedNameEngine returns a name engine backed by store (nil → an
// in-memory metastore, so the engine still works for placeholder shares).
func NewDerivedNameEngine(store metastore.Store) NameEngine {
	if store == nil {
		store, _ = metastore.NewMem("")
	}
	return &derivedNameEngine{store: store}
}

// metastore key layout, prefixed so multiple name kinds share one store without
// colliding:
//
//	"n/f/<kind>/<dir>/<LONG>"    -> derived   (forward: long  -> derived)
//	"n/r/<kind>/<dir>/<DERIVED>" -> long      (reverse: derived -> long)
//
// Both keys are CASE-FOLDED (upper-cased), so name resolution is case-insensitive
// the way a DOS/Windows filesystem is: "Report.txt" and "REPORT.TXT" hash to the
// same forward slot and therefore COLLIDE — the second long name to arrive gets a
// fresh ~N / -N suffix rather than silently aliasing the first. The VALUE stored
// is the original-case long name, so medium names round-trip in their stored case
// (Windows-FS semantics: preserved case, insensitive lookup). This is identical on
// Windows, macOS, and Linux — the engine never consults the host's case rules.
func fwdKey(kind NameKind, dir, long string) []byte {
	return []byte("n/f/" + kindTag(kind) + "/" + foldDir(dir) + "/" + strings.ToUpper(long))
}

func revKey(kind NameKind, dir, derived string) []byte {
	return []byte("n/r/" + kindTag(kind) + "/" + foldDir(dir) + "/" + strings.ToUpper(derived))
}

// foldDir case-folds a directory path for the key, so a parent directory whose
// own casing varies between requests still scopes its children to one namespace.
func foldDir(dir string) string { return strings.ToUpper(dir) }

func kindTag(kind NameKind) string {
	if kind == MediumName {
		return "m"
	}
	return "s"
}

// Bind returns the derived name for long in dir, allocating and persisting a
// fresh one (with ~N / -N collision suffixes) the first time. When long
// already fits the target convention (8.3 for ShortName, <=31 chars for
// MediumName) with no sanitization needed, it is bound and returned as-is —
// no suffix is manufactured for a name that doesn't need one.
func (e *derivedNameEngine) Bind(dir, long string, kind NameKind) string {
	if existing, ok := e.store.Get(fwdKey(kind, dir, long)); ok {
		return string(existing)
	}

	if asIs, ok := fitsAsIs(long, kind); ok {
		rk := revKey(kind, dir, asIs)
		if owner, taken := e.store.Get(rk); !taken || string(owner) == long {
			_ = e.store.Put(fwdKey(kind, dir, long), []byte(asIs))
			_ = e.store.Put(rk, []byte(long))
			return asIs
		}
	}

	maxN := 1 << 16
	for n := 1; n < maxN; n++ {
		var cand string
		if kind == MediumName {
			cand = deriveMedium(long, n)
		} else {
			cand = derive83(long, n)
		}
		rk := revKey(kind, dir, cand)
		if owner, taken := e.store.Get(rk); taken {
			if string(owner) == long {
				return cand // already ours
			}
			continue // collision with a different long name; try next suffix
		}
		_ = e.store.Put(fwdKey(kind, dir, long), []byte(cand))
		_ = e.store.Put(rk, []byte(long))
		return cand
	}
	return long
}

// fitsAsIs reports whether long already satisfies kind's naming convention
// without any truncation or character sanitization, so it can be bound to a
// stable form (asIs) instead of growing a synthetic ~N / -N suffix. For
// ShortName, asIs is the DOS-cased (uppercased) form of long, since 8.3 short
// names are case-insensitive and always stored/displayed upper-case.
func fitsAsIs(long string, kind NameKind) (asIs string, ok bool) {
	if kind == MediumName {
		return long, len(long) <= 31
	}
	base, ext := splitExt(long)
	upperBase, upperExt := strings.ToUpper(base), strings.ToUpper(ext)
	if base == "" || len(upperBase) > 8 || len(upperExt) > 3 {
		return "", false
	}
	if upperBase != sanitizeFAT(upperBase) || upperExt != sanitizeFAT(upperExt) {
		return "", false
	}
	out := upperBase
	if upperExt != "" {
		out += "." + upperExt
	}
	return out, true
}

// ToLong reverses Bind: the long name a derived name maps to in dir.
func (e *derivedNameEngine) ToLong(dir, derived string, kind NameKind) (string, bool) {
	if v, ok := e.store.Get(revKey(kind, dir, derived)); ok {
		return string(v), true
	}
	return derived, false
}

// --- 8.3 short-name derivation (ported from pkg/shortname) ---

// derive83 produces a deterministic 8.3 candidate from long with collision
// counter n (encoded as ~n). Uniqueness is the caller's responsibility.
func derive83(long string, n int) string {
	base, ext := splitExt(long)
	base = sanitizeFAT(strings.ToUpper(base))
	ext = sanitizeFAT(strings.ToUpper(ext))
	if len(ext) > 3 {
		ext = ext[:3]
	}
	suffix := "~" + strconv.Itoa(n)
	keep := max(8-len(suffix), 1)
	if len(base) > keep {
		base = base[:keep]
	}
	if base == "" {
		base = "FILE"
		if len(base) > keep {
			base = base[:keep]
		}
	}
	out := base + suffix
	if ext != "" {
		out += "." + ext
	}
	return out
}

// deriveMedium produces a 31-character "medium" name (the classic AFP long-name
// limit) from long, appending a "-n" suffix for collisions n > 1.
func deriveMedium(long string, n int) string {
	const limit = 31
	name := long
	suffix := ""
	if n > 1 {
		suffix = "-" + strconv.Itoa(n)
	}
	if len(name)+len(suffix) > limit {
		name = name[:max(limit-len(suffix), 0)]
	}
	return name + suffix
}

func splitExt(name string) (base, ext string) {
	idx := strings.LastIndex(name, ".")
	if idx <= 0 || idx == len(name)-1 {
		return name, ""
	}
	return name[:idx], name[idx+1:]
}

// sanitizeFAT strips characters illegal in FAT 8.3 short names. Intentionally
// simple — the canonical Windows mapping is more elaborate.
func sanitizeFAT(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-' || r == '$' || r == '#' || r == '&' || r == '@' ||
			r == '!' || r == '(' || r == ')' || r == '{' || r == '}' || r == '\'' || r == '`':
			b.WriteRune(r)
		default:
			// Drop spaces, dots (handled), and anything else.
		}
	}
	return b.String()
}
