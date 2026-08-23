package fs

import "strings"

// foldresolve.go provides case-insensitive store-path resolution for the file
// services that need it (NetWare DOS/OS2/MAC name spaces, and any caller that
// wants Windows/Mac-style case-insensitive matching on a case-sensitive host).
//
// Why it exists: a store path is '/'-separated and share-relative, but whether a
// lookup of "REPORT.TXT" finds an on-disk "Report.txt" depends on the HOST file
// system's case rules — case-insensitive on NTFS/APFS, case-SENSITIVE on ext4. To
// honour the legacy "case-insensitive filename" contract (DOS/OS2/MAC clients
// expect it; only NFS is case-sensitive) regardless of host, ResolveFold folds
// each component by scanning its parent directory for a case-insensitive match.
// It is the protocol-neutral equivalent of mars_nwe's VOL_OPTION_IGNCASE
// directory-scan fold.
//
// It works through the FileSystem interface only (ReadDir), so it resolves for
// ANY backend — local_fs, memfs, an image backend — without each backend
// re-implementing case folding. There is deliberately NO "does Stat(storePath)
// succeed" fast path: on a case-insensitive host (Windows/NTFS, macOS/APFS)
// Stat succeeds for any casing that matches an existing entry, and Go's os.Stat
// does not correct the returned FileInfo's name back to the on-disk spelling —
// a fast path built on that assumption silently returned the CALLER's casing
// as if it were the canonical stored name, which every metastore-backed lookup
// keyed by store path (EAs, DOS attributes, CNIDs — all plain case-sensitive
// string keys, unlike case-insensitive file I/O) then silently missed against.
// See foldComponent's doc comment for the concrete regression this caused.

// ResolveFold returns the store path whose components match storePath
// case-insensitively, resolving each element against what is actually on the
// backend. It returns (resolved, true) when every component resolves (the leaf may
// resolve to an existing entry), or (storePath, false) when some component does not
// exist — in which case the caller uses storePath as-is (e.g. a create at the
// requested casing).
func ResolveFold(fsys FileSystem, storePath string) (string, bool) {
	clean := strings.Trim(storePath, "/")
	if clean == "" {
		return storePath, true // the volume root
	}

	parts := strings.Split(clean, "/")
	resolved := make([]string, 0, len(parts))
	dir := ""
	for i, want := range parts {
		if want == "" {
			continue
		}
		actual, ok := foldComponent(fsys, dir, want)
		if !ok {
			// This component does not exist. Keep the requested casing for it and the
			// rest (a create target), and report not-fully-resolved.
			out := append(resolved, parts[i:]...) //nolint:gocritic // resolved is not read again; the function returns on the next line
			return strings.Join(out, "/"), false
		}
		resolved = append(resolved, actual)
		dir = strings.Join(resolved, "/")
	}
	return strings.Join(resolved, "/"), true
}

// foldComponent returns the actual stored name of the child of dir that matches
// want case-insensitively, by scanning dir for it.
//
// A "does Stat(dir/want) succeed" fast path was tried here and removed: it is
// unsound on any case-insensitive host filesystem (Windows/NTFS, macOS/APFS —
// exactly the common local_fs deployment targets). Stat succeeding only proves
// the path EXISTS under that spelling; on a case-insensitive host it succeeds
// for ANY casing that matches an existing entry, and Go's os.Stat does not
// correct the returned FileInfo's name back to the on-disk spelling. Trusting
// it as "the real stored name" let it return the CALLER's casing unchanged —
// e.g. a client's TRANS2_QUERY_PATH_INFORMATION for "1516HBWT.CAB" resolved to
// store path "1516HBWT.CAB" even though the file was created (and its EAs
// keyed) under "1516HBWT.cab", because Stat("1516HBWT.CAB") happily succeeded
// on NTFS. Everything downstream keyed by the resolved store path (EA/DOS-attr
// metastore lookups, which — unlike file I/O — ARE case-sensitive, plain
// string keys) then silently missed: OS/2 WPS set a .ICON EA, queried it back
// under different casing moments later, and got an empty placeholder instead
// of the value it had just written (netbeui.pcap 2026-07-15 frames 513-522).
// A directory scan is the only way to recover the true stored name on a
// case-insensitive host — there is no cheaper syscall; Go's ReadDir is already
// the FindFirstFileExW-equivalent path on Windows.
func foldComponent(fsys FileSystem, dir, want string) (string, bool) {
	entries, err := fsys.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), want) {
			return e.Name(), true
		}
	}
	return "", false
}
