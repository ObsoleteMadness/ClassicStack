// Package metastore is the one keyed-store interface CNID, shortname, and
// desktop all share, plus the default mem-snapshot-to-file implementation so
// embedded/TinyGo builds can drop sqlite (§9a).
//
// Ring: CORE (stdlib only — sqlite is just one adapter). Real types land in
// step B9.
package metastore
