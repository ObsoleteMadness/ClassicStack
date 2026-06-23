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
// expect it; only NFS is case-sensitive) regardless of host, ResolveFold tries the
// path as given first and, on a miss, folds each component by scanning its parent
// directory for a case-insensitive match. It is the protocol-neutral equivalent of
// mars_nwe's VOL_OPTION_IGNCASE directory-scan fold.
//
// It works through the FileSystem interface only (Stat + ReadDir), so it resolves
// for ANY backend — local_fs, memfs, an image backend — without each backend
// re-implementing case folding. On a case-insensitive host the exact Stat succeeds
// first try, so the scan is a Linux-only slow path.

// ResolveFold returns the store path whose components match storePath
// case-insensitively, resolving each element against what is actually on the
// backend. It returns (resolved, true) when every component resolves (the leaf may
// resolve to an existing entry), or (storePath, false) when some component does not
// exist — in which case the caller uses storePath as-is (e.g. a create at the
// requested casing).
//
// The fast path: if fsys.Stat(storePath) succeeds, the path already matches and is
// returned unchanged. Only on a miss does it walk and fold, so a case-insensitive
// host (or an exact-case request) pays nothing.
func ResolveFold(fsys FileSystem, storePath string) (string, bool) {
	clean := strings.Trim(storePath, "/")
	if clean == "" {
		return storePath, true // the volume root
	}
	// Fast path: an exact match needs no folding.
	if _, err := fsys.Stat(clean); err == nil {
		return clean, true
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
			out := append(resolved, parts[i:]...)
			return strings.Join(out, "/"), false
		}
		resolved = append(resolved, actual)
		dir = strings.Join(resolved, "/")
	}
	return strings.Join(resolved, "/"), true
}

// foldComponent returns the actual stored name of the child of dir that matches
// want case-insensitively. An exact hit is preferred (and avoids a directory scan);
// otherwise it scans dir for the first EqualFold match.
func foldComponent(fsys FileSystem, dir, want string) (string, bool) {
	exact := want
	if dir != "" {
		exact = dir + "/" + want
	}
	if _, err := fsys.Stat(exact); err == nil {
		return want, true
	}
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
