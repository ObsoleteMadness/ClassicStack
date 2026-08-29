package smb

import "strings"

// --- DOS/SMB wildcard matching for directory enumeration. The classic SMB
// search wildcards are '*' (any run, including empty) and '?' (exactly one
// character); matching is case-insensitive, as the DOS/Win9x filesystem the
// clients expect is. The legacy service used the same semantics; this is the
// store-charset re-expression used by the TRANS2 find path. ---

// wildcardMatch reports whether name matches a DOS-style pattern. An empty
// pattern or "*"/"*.*" matches everything. Comparison is case-insensitive.
//
// Matching is 8.3-segmented: the name and the pattern are each split on their
// first '.' into a base and an extension, and each segment is matched
// independently. This is what the CORE-dialect clients expect. WfW 3.11 browses a
// folder by sending FileName "????????.???" (eight '?' + dot + three '?'), and a
// dotless directory name like "SUBA" must match it — so '?' matches one character
// OR nothing when the name's segment has run short, and a name with no extension
// still matches a pattern whose extension segment is all wildcards. A plain
// left-to-right glob (where '.' is a literal that a dotless name can never
// satisfy) drops every extensionless directory, which is the "6 files and no
// directories" browse failure. See spec/errata.md "SMB_COM_SEARCH 8.3 matching".
func wildcardMatch(name, pattern string) bool {
	if pattern == "" || pattern == "*" || pattern == "*.*" {
		return true
	}
	pBase, pExt := splitDOSPattern(pattern)
	nBase, nExt := splitDOSPattern(name)
	return matchDOSSegment(nBase, pBase) && matchDOSSegment(nExt, pExt)
}

// splitDOSPattern splits an 8.3 name or pattern into its base and extension at
// the first '.'. A name with no dot has an empty extension.
func splitDOSPattern(s string) (base, ext string) {
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		return s[:dot], s[dot+1:]
	}
	return s, ""
}

// matchDOSSegment matches a single 8.3 component (base or extension),
// case-insensitively. '*' matches the rest of the segment greedily; '?' consumes
// one character of the name, or matches an early end-of-name (the DOS quirk that
// lets "????????.???" match a short or dotless name); any other character must
// match. The segment matches only when the whole name segment is consumed.
func matchDOSSegment(name, pattern string) bool {
	var ni, pi int
	for pi < len(pattern) {
		switch pattern[pi] {
		case '*':
			return true // greedy: matches whatever remains in this segment
		case '?':
			if ni < len(name) {
				ni++
			}
			pi++
		default:
			if ni >= len(name) || toLowerASCII(pattern[pi]) != toLowerASCII(name[ni]) {
				return false
			}
			ni++
			pi++
		}
	}
	return ni == len(name)
}

// toLowerASCII lowercases an ASCII byte; non-letters pass through. DOS 8.3 names
// are ASCII, so byte-wise folding is exact here.
func toLowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
