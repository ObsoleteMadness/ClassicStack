//go:build afp

package afp

import (
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const defaultAppleDoubleMode = AppleDoubleModeModern

// AppleDoubleBackend stores AFP metadata/resource forks in AppleDouble files.
// Mode controls path layout:
// - netatalk modern: sidecar files named ._name in the same directory.
// - netatalk legacy: files under .AppleDouble/name in the same directory.
type AppleDoubleBackend struct {
	fs              FileSystem
	mode            AppleDoubleMode
	decomposedNames bool
}

func NewAppleDoubleBackend(fs FileSystem, mode AppleDoubleMode, decomposedNames bool) *AppleDoubleBackend {
	if mode == "" {
		mode = defaultAppleDoubleMode
	}
	if mode != AppleDoubleModeLegacy {
		mode = AppleDoubleModeModern
	}
	return &AppleDoubleBackend{fs: fs, mode: mode, decomposedNames: decomposedNames}
}

func resolveForkMetadataBackend(options Options, fs FileSystem) ForkMetadataBackend {
	if options.ForkMetadataBackend != nil {
		return options.ForkMetadataBackend
	}
	if fs == nil {
		return nil
	}
	return NewAppleDoubleBackend(fs, options.AppleDoubleMode, options.DecomposedFilenames)
}

// MetadataPath returns the AppleDouble sidecar path for the given host file path.
// If filePath is already a sidecar path, it is returned in canonical form.
func (b *AppleDoubleBackend) MetadataPath(filePath string) string {
	filePath = b.ownerPath(filePath)
	dir := filepath.Dir(filePath)
	base := filepath.Base(filePath)
	if b.mode == AppleDoubleModeLegacy {
		return filepath.Join(dir, ".AppleDouble", base)
	}
	return filepath.Join(dir, "._"+base)
}

// metadataPath is the unexported alias used internally for brevity.
func (b *AppleDoubleBackend) metadataPath(filePath string) string {
	return b.MetadataPath(filePath)
}

// ownerPath maps either a host file path or an AppleDouble sidecar path back
// to the logical host file path used to derive the sidecar name.
func (b *AppleDoubleBackend) ownerPath(filePath string) string {
	clean := filepath.Clean(filePath)
	base := filepath.Base(clean)

	if b.mode == AppleDoubleModeLegacy {
		dir := filepath.Dir(clean)
		if strings.EqualFold(filepath.Base(dir), ".AppleDouble") {
			return filepath.Join(filepath.Dir(dir), base)
		}
		return clean
	}

	if strings.HasPrefix(base, "._") {
		return filepath.Join(filepath.Dir(clean), strings.TrimPrefix(base, "._"))
	}
	return clean
}

func (b *AppleDoubleBackend) IsMetadataArtifact(name string, isDir bool) bool {
	if strings.HasPrefix(name, "._") {
		return true
	}
	if b.mode == AppleDoubleModeLegacy && strings.EqualFold(name, ".AppleDouble") {
		return true
	}
	return false
}

// IconFileName returns the host filesystem name for the Mac "Icon\r" file.
// In legacy AppleDouble mode netatalk stored this as "Icon_".
// In modern mode with decomposed filenames the 0x0D is escaped as "Icon0x0D".
// In modern mode without decomposed filenames the literal "\r" is preserved.
func (b *AppleDoubleBackend) IconFileName() string {
	if b.mode == AppleDoubleModeLegacy {
		return "Icon_"
	}
	if b.decomposedNames {
		return "Icon0x0D"
	}
	return "Icon\r"
}

// allIconFileNames returns every possible host representation of the Mac
// "Icon\r" file. Used by iconAliasPath to recognise any variant and remap
// it to the canonical form returned by IconFileName.
func allIconFileNames() []string {
	return []string{"Icon0x0D", "Icon_", "Icon\r"}
}

// isIconFile reports whether name is any host representation of Icon\r.
func isIconFile(name string) bool {
	for _, n := range allIconFileNames() {
		if name == n {
			return true
		}
	}
	return false
}

// iconAliasPath returns the canonical host path for an Icon\r file if path
// refers to a non-canonical variant. Returns "" when path is not an Icon
// file or is already in canonical form.
func (b *AppleDoubleBackend) iconAliasPath(path string) string {
	base := filepath.Base(path)
	canonical := b.IconFileName()
	if !isIconFile(base) || base == canonical {
		return ""
	}
	return filepath.Join(filepath.Dir(path), canonical)
}

func (b *AppleDoubleBackend) StatWithMetadataFallback(path string) (string, fs.FileInfo, error) {
	info, err := b.fs.Stat(path)
	if err == nil {
		return path, info, nil
	}

	if aliasPath := b.iconAliasPath(path); aliasPath != "" {
		aliasInfo, aliasErr := b.fs.Stat(aliasPath)
		if aliasErr == nil {
			return aliasPath, aliasInfo, nil
		}

		aliasMetaPath := b.metadataPath(aliasPath)
		aliasMetaInfo, aliasMetaErr := b.fs.Stat(aliasMetaPath)
		if aliasMetaErr == nil {
			return aliasMetaPath, aliasMetaInfo, nil
		}
	}

	base := filepath.Base(path)
	if strings.HasPrefix(base, "._") {
		return path, nil, err
	}

	altPath := b.metadataPath(path)
	altInfo, altErr := b.fs.Stat(altPath)
	if altErr == nil {
		return altPath, altInfo, nil
	}

	return path, nil, err
}

func (b *AppleDoubleBackend) ReadForkMetadata(path string) (ForkMetadata, error) {
	adData := b.readAppleDoubleDataPath(b.metadataPath(path))
	return ForkMetadata{
		FinderInfo:      adData.finderInfo,
		ResourceForkLen: adData.rsrcLength,
		HasResourceFork: adData.hasRsrc,
	}, nil
}

func (b *AppleDoubleBackend) WriteFinderInfo(path string, finderInfo [32]byte) error {
	return b.writeFinderInfoPath(b.metadataPath(path), finderInfo)
}

func (b *AppleDoubleBackend) OpenResourceFork(path string, writable bool) (File, ResourceForkInfo, error) {
	adPath := b.metadataPath(path)
	adData := b.readAppleDoubleDataPath(adPath)
	if adData.hasRsrc {
		if writable {
			f, err := b.fs.OpenFile(adPath, os.O_RDWR)
			if err != nil {
				f, err = b.fs.OpenFile(adPath, os.O_RDONLY)
			}
			if err != nil {
				return nil, ResourceForkInfo{}, err
			}
			return f, ResourceForkInfo{
				Offset:            adData.rsrcOffset,
				Length:            adData.rsrcLength,
				LengthFieldOffset: adData.rsrcLenFieldAt,
			}, nil
		}

		f, err := b.fs.OpenFile(adPath, os.O_RDONLY)
		if err != nil {
			return nil, ResourceForkInfo{}, err
		}
		return f, ResourceForkInfo{
			Offset:            adData.rsrcOffset,
			Length:            adData.rsrcLength,
			LengthFieldOffset: adData.rsrcLenFieldAt,
		}, nil
	}

	if !writable {
		return nil, ResourceForkInfo{}, nil
	}

	if err := b.createAppleDoublePath(adPath); err != nil {
		return nil, ResourceForkInfo{}, err
	}
	f, err := b.fs.OpenFile(adPath, os.O_RDWR)
	if err != nil {
		return nil, ResourceForkInfo{}, err
	}
	return f, ResourceForkInfo{
		Offset:            int64(adResourceForkStart),
		Length:            0,
		LengthFieldOffset: adRsrcLenFileOffset,
	}, nil
}

func (b *AppleDoubleBackend) TruncateResourceFork(file File, info ResourceForkInfo, newLen int64) error {
	if err := file.Truncate(info.Offset + newLen); err != nil {
		return err
	}

	lenFieldAt := info.LengthFieldOffset
	if lenFieldAt == 0 {
		lenFieldAt = adRsrcLenFileOffset
	}

	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(newLen))
	if _, err := file.WriteAt(lenBuf, lenFieldAt); err != nil {
		return err
	}
	return file.Sync()
}

func (b *AppleDoubleBackend) MoveMetadata(oldpath, newpath string) error {
	oldMeta := b.metadataPath(oldpath)
	newMeta := b.metadataPath(newpath)
	if b.mode == AppleDoubleModeLegacy {
		err := b.fs.CreateDir(filepath.Dir(newMeta))
		if err != nil && !os.IsExist(err) {
			return err
		}
	}
	if err := b.fs.Rename(oldMeta, newMeta); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (b *AppleDoubleBackend) DeleteMetadata(path string) error {
	if err := b.fs.Remove(b.metadataPath(path)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (b *AppleDoubleBackend) CopyMetadata(srcPath, dstPath string) error {
	return b.CopyMetadataFrom(b, srcPath, dstPath)
}

func (b *AppleDoubleBackend) CopyMetadataFrom(source ForkMetadataBackend, srcPath, dstPath string) error {
	if source == nil {
		return nil
	}

	if srcBackend, ok := source.(*AppleDoubleBackend); ok {
		return b.copyAppleDoubleSidecar(srcBackend, srcPath, dstPath)
	}

	return b.copyMetadataGeneric(source, srcPath, dstPath)
}

func (b *AppleDoubleBackend) copyAppleDoubleSidecar(source *AppleDoubleBackend, srcPath, dstPath string) error {
	srcMeta := source.metadataPath(srcPath)
	dstMeta := b.metadataPath(dstPath)

	srcFile, err := source.fs.OpenFile(srcMeta, os.O_RDONLY)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer srcFile.Close()

	if err := b.ensureAppleDoubleDir(dstMeta); err != nil {
		return err
	}

	dstFile, err := b.fs.CreateFile(dstMeta)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	buf := make([]byte, 32768)
	var offset int64
	for {
		n, readErr := srcFile.ReadAt(buf, offset)
		if n > 0 {
			if _, writeErr := dstFile.WriteAt(buf[:n], offset); writeErr != nil {
				return writeErr
			}
			offset += int64(n)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return dstFile.Sync()
}

func (b *AppleDoubleBackend) copyMetadataGeneric(source ForkMetadataBackend, srcPath, dstPath string) error {
	metadata, err := source.ReadForkMetadata(srcPath)
	if err != nil {
		return err
	}

	hasFinder := hasFinderInfo(metadata.FinderInfo)

	var (
		comment    []byte
		hasComment bool
	)
	if cb, ok := source.(CommentBackend); ok {
		comment, hasComment = cb.ReadComment(srcPath)
	}

	srcFork, srcForkInfo, err := source.OpenResourceFork(srcPath, false)
	if err != nil {
		return err
	}
	if srcFork != nil {
		defer srcFork.Close()
	}

	rsrcLen := metadata.ResourceForkLen
	if srcForkInfo.Length > rsrcLen {
		rsrcLen = srcForkInfo.Length
	}
	hasRsrc := metadata.HasResourceFork || srcFork != nil || rsrcLen > 0

	if !hasFinder && !hasComment && !hasRsrc {
		return nil
	}

	if hasFinder {
		if err := b.WriteFinderInfo(dstPath, metadata.FinderInfo); err != nil {
			return err
		}
	}
	if hasComment {
		if err := b.WriteComment(dstPath, comment); err != nil {
			return err
		}
	}
	if !hasRsrc {
		return nil
	}

	dstFork, dstForkInfo, err := b.OpenResourceFork(dstPath, true)
	if err != nil {
		return err
	}
	if dstFork == nil {
		return nil
	}
	defer dstFork.Close()

	if srcFork != nil && rsrcLen > 0 {
		if err := copyForkBytes(srcFork, srcForkInfo.Offset, dstFork, dstForkInfo.Offset, rsrcLen); err != nil {
			return err
		}
	}

	return b.TruncateResourceFork(dstFork, dstForkInfo, rsrcLen)
}

func hasFinderInfo(finderInfo [32]byte) bool {
	for _, b := range finderInfo {
		if b != 0 {
			return true
		}
	}
	return false
}

func copyForkBytes(src File, srcOffset int64, dst File, dstOffset int64, length int64) error {
	buf := make([]byte, 32768)
	var copied int64
	for copied < length {
		chunk := buf
		remaining := length - copied
		if remaining < int64(len(chunk)) {
			chunk = chunk[:remaining]
		}

		n, readErr := src.ReadAt(chunk, srcOffset+copied)
		if n > 0 {
			if _, writeErr := dst.WriteAt(chunk[:n], dstOffset+copied); writeErr != nil {
				return writeErr
			}
			copied += int64(n)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
		if n == 0 {
			break
		}
	}
	return nil
}

func (b *AppleDoubleBackend) ExchangeMetadata(pathA, pathB string) error {
	metaA := b.metadataPath(pathA)
	metaB := b.metadataPath(pathB)

	_, errA := b.fs.Stat(metaA)
	hasA := errA == nil
	_, errB := b.fs.Stat(metaB)
	hasB := errB == nil

	if !hasA && !hasB {
		return nil
	}

	if b.mode == AppleDoubleModeLegacy {
		err := b.fs.CreateDir(filepath.Dir(metaA))
		if err != nil && !os.IsExist(err) {
			return err
		}
		err = b.fs.CreateDir(filepath.Dir(metaB))
		if err != nil && !os.IsExist(err) {
			return err
		}
	}

	tmp := metaA + ".__afp_meta_swap__"
	if hasA {
		if err := b.fs.Rename(metaA, tmp); err != nil {
			return err
		}
	}

	if hasB {
		if err := b.fs.Rename(metaB, metaA); err != nil {
			if hasA {
				_ = b.fs.Rename(tmp, metaA)
			}
			return err
		}
	}

	if hasA {
		if err := b.fs.Rename(tmp, metaB); err != nil {
			return err
		}
	}

	return nil
}

// ReadComment reads the Finder comment (AppleDouble entry ID 4) from the sidecar
// for path, using the configured mode to locate the sidecar file.
func (b *AppleDoubleBackend) ReadComment(path string) ([]byte, bool) {
	return b.readAppleDoubleCommentPath(b.metadataPath(path))
}

// WriteComment writes a Finder comment into the AppleDouble sidecar for path,
// creating the sidecar (and the .AppleDouble directory in legacy mode) if needed.
func (b *AppleDoubleBackend) WriteComment(path string, comment []byte) error {
	return b.writeAppleDoubleCommentPath(b.metadataPath(path), comment)
}

// RemoveComment clears the Finder comment from the AppleDouble sidecar for path.
func (b *AppleDoubleBackend) RemoveComment(path string) error {
	return b.removeAppleDoubleCommentPath(b.metadataPath(path))
}

func (b *AppleDoubleBackend) ensureAppleDoubleDir(adPath string) error {
	err := b.fs.CreateDir(filepath.Dir(adPath))
	if err != nil && !os.IsExist(err) {
		return err
	}
	return nil
}

func (b *AppleDoubleBackend) readFile(path string) ([]byte, error) {
	f, err := b.fs.OpenFile(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	size := info.Size()
	if size < 0 {
		size = 0
	}

	buf := make([]byte, size)
	var off int64
	for off < size {
		n, readErr := f.ReadAt(buf[off:], off)
		if n > 0 {
			off += int64(n)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	if len(buf) < adHeaderSize {
		return nil, io.ErrUnexpectedEOF
	}
	return buf, nil
}

func (b *AppleDoubleBackend) writeFile(path string, data []byte) error {
	f, err := b.fs.CreateFile(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if len(data) > 0 {
		if _, err := f.WriteAt(data, 0); err != nil {
			return err
		}
	}
	return f.Sync()
}

func (b *AppleDoubleBackend) createAppleDoublePath(adPath string) error {
	if err := b.ensureAppleDoubleDir(adPath); err != nil {
		return err
	}
	return b.writeFile(adPath, buildAppleDoubleBytes(parsedAppleDouble{}, false, 0))
}

func (b *AppleDoubleBackend) readAppleDoubleDataPath(adPath string) appleDoubleData {
	var result appleDoubleData
	bts, err := b.readFile(adPath)
	if err != nil {
		return result
	}

	parsed, err := parseAppleDoubleBytes(bts)
	if err != nil {
		return result
	}

	if parsed.HasFinder {
		result.finderInfo = parsed.FinderInfo
	}
	if parsed.HasResource {
		result.rsrcOffset = parsed.ResourceOffset
		result.rsrcLength = int64(len(parsed.Resource))
		result.rsrcLenFieldAt = parsed.ResourceLenAt
		result.hasRsrc = true
	}
	return result
}

func (b *AppleDoubleBackend) writeFinderInfoPath(adPath string, fi [32]byte) error {
	bts, err := b.readFile(adPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := b.createAppleDoublePath(adPath); err != nil {
			return err
		}
		bts, err = b.readFile(adPath)
		if err != nil {
			return err
		}
	}

	parsed, _ := parseAppleDoubleBytes(bts)
	parsed.FinderInfo = fi
	parsed.HasFinder = true

	out := buildAppleDoubleBytes(parsed, parsed.HasComment, uint32(len(parsed.Comment)))
	return b.writeFile(adPath, out)
}

func (b *AppleDoubleBackend) writeAppleDoubleCommentPath(adPath string, comment []byte) error {
	bts, err := b.readFile(adPath)
	if err != nil {
		if err := b.createAppleDoublePath(adPath); err != nil {
			return err
		}
		bts, err = b.readFile(adPath)
		if err != nil {
			return err
		}
	}

	parsed, _ := parseAppleDoubleBytes(bts)
	if len(comment) > 199 {
		comment = comment[:199]
	}
	parsed.Comment = append([]byte(nil), comment...)
	parsed.HasComment = len(comment) > 0

	out := buildAppleDoubleBytes(parsed, true, uint32(len(comment)))
	return b.writeFile(adPath, out)
}

func (b *AppleDoubleBackend) removeAppleDoubleCommentPath(adPath string) error {
	bts, err := b.readFile(adPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	parsed, _ := parseAppleDoubleBytes(bts)
	parsed.Comment = nil
	parsed.HasComment = false

	out := buildAppleDoubleBytes(parsed, true, 0)
	return b.writeFile(adPath, out)
}

func (b *AppleDoubleBackend) readAppleDoubleCommentPath(adPath string) ([]byte, bool) {
	bts, err := b.readFile(adPath)
	if err != nil {
		return nil, false
	}
	parsed, err := parseAppleDoubleBytes(bts)
	if err != nil {
		return nil, false
	}
	if !parsed.HasComment || len(parsed.Comment) == 0 {
		return nil, false
	}
	if len(parsed.Comment) > 128 {
		return parsed.Comment[:128], true
	}
	return parsed.Comment, true
}
