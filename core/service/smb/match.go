package smb

import "strings"

// --- DOS/SMB wildcard matching for directory enumeration. The classic SMB
// search wildcards are '*' (any run, including empty) and '?' (exactly one
// character); matching is case-insensitive, as the DOS/Win9x filesystem the
// clients expect is. The legacy service used the same semantics; this is the
// store-charset re-expression used by the TRANS2 find path. ---

// wildcardMatch reports whether name matches a DOS-style pattern. An empty
// pattern or "*"/"*.*" matches everything; '?' matches one rune, '*' matches any
// run. Comparison is case-insensitive. The DOS quirk that a trailing "." in the
// pattern also matches an extensionless name is handled by normalising "*.*" to
// "*" up front (every file matches it).
func wildcardMatch(name, pattern string) bool {
	if pattern == "" || pattern == "*" || pattern == "*.*" {
		return true
	}
	return matchFold([]rune(strings.ToLower(name)), []rune(strings.ToLower(pattern)))
}

// matchFold runs the classic two-pointer wildcard match with '*' backtracking,
// on already-lowercased runes.
func matchFold(name, pat []rune) bool {
	var ni, pi int
	star, mark := -1, 0
	for ni < len(name) {
		switch {
		case pi < len(pat) && (pat[pi] == '?' || pat[pi] == name[ni]):
			ni++
			pi++
		case pi < len(pat) && pat[pi] == '*':
			star = pi
			mark = ni
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			ni = mark
		default:
			return false
		}
	}
	for pi < len(pat) && pat[pi] == '*' {
		pi++
	}
	return pi == len(pat)
}
