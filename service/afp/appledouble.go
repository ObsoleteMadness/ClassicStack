package afp

// AppleDouble format helpers now live in pkg/appledouble. The aliases
// below preserve the historical AFP-package identifiers while the
// surrounding fork/desktop code is migrated piecemeal.

import (
	"github.com/pgodw/omnitalk/pkg/appledouble"
)

const (
	adMagic   = appledouble.Magic
	adVersion = appledouble.Version

	adEntryIDDataFork     = appledouble.EntryIDDataFork
	adEntryIDFinderInfo   = appledouble.EntryIDFinderInfo
	adEntryIDResourceFork = appledouble.EntryIDResourceFork
	adEntryIDComment      = appledouble.EntryIDComment
	adEntryIDIconBW       = appledouble.EntryIDIconBW

	adHeaderSize = appledouble.HeaderSize
	adEntrySize  = appledouble.EntrySize

	adFinderInfoOffset  = appledouble.FinderInfoOffset
	adResourceForkStart = appledouble.ResourceForkStart
	adRsrcLenFileOffset = appledouble.ResourceLenFileOffset
)

// appleDoublePath returns the modern (._name) sidecar path for filePath.
func appleDoublePath(filePath string) string {
	return appledouble.SidecarPath(filePath)
}

// parsedAppleDouble is the legacy package-local alias for the format
// struct now defined in pkg/appledouble. New code should use
// appledouble.Parsed directly.
type parsedAppleDouble = appledouble.Parsed

// appleDoubleData is the legacy slim summary used by fork I/O paths.
// It is retained for the existing call sites; the format-level data
// lives on parsedAppleDouble.
type appleDoubleData struct {
	finderInfo     [32]byte
	rsrcOffset     int64
	rsrcLength     int64
	rsrcLenFieldAt int64
	hasRsrc        bool
}

func parseAppleDoubleBytes(b []byte) (parsedAppleDouble, error) {
	return appledouble.Parse(b)
}

func buildAppleDoubleBytes(p parsedAppleDouble, includeCommentEntry bool, commentLen uint32) []byte {
	return appledouble.Build(p, includeCommentEntry, commentLen)
}
