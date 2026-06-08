// Package fs is the single filesystem seam AFP and SMB consume: FileSystem/File,
// the per-share-swappable fork engine, name engine, and filename codec, the
// per-share assembly (BuildShare), and the FS-mutation bus instance (§9/§10).
//
// Ring: CORE (stdlib + core/bus + core/metastore). Real types land in steps
// B4 (the FS-mutation bus) and B8 (the FS interface family).
package fs
