package fuse

import (
	"fmt"
	"runtime"
	"strings"
)

// XattrLayout selects which native-fork xattr names the mount advertises.
type XattrLayout int

const (
	// XattrLayoutHost picks Apple names on Darwin and Netatalk names on Linux.
	XattrLayoutHost XattrLayout = iota
	// XattrLayoutApple is com.apple.FinderInfo + com.apple.ResourceFork.
	XattrLayoutApple
	// XattrLayoutNetatalk is user.org.netatalk.Metadata + ResourceFork.
	XattrLayoutNetatalk
)

// Options carries the mount-time knobs.
type Options struct {
	// VolumeLabel is the label shown for the mounted volume (empty → "ClassicStack").
	VolumeLabel string
	// ReadOnly forces a read-only mount even if the ForkFS itself is writable.
	ReadOnly bool
	// NativeForks surfaces a file's resource fork and Apple metadata as
	// host-native xattrs (Apple on Darwin, Netatalk on Linux). csmount sets it
	// for passthrough/native/hfs/ads/xattr.
	NativeForks bool
	// Layout selects the xattr name table. Zero (XattrLayoutHost) follows GOOS.
	// Tests set Apple or Netatalk explicitly so both tables run on every OS.
	Layout XattrLayout
}

func (o Options) resolvedLayout() XattrLayout {
	if o.Layout != XattrLayoutHost {
		return o.Layout
	}
	return hostXattrLayout()
}

// fuseHostArgs is the cgofuse option vector passed to FileSystemHost.Mount.
// volname/fsname values are escaped so spaces and commas survive macFUSE's
// comma-separated -o parser (a leaf like "OpenRetroSCSI 7.5.3" is one option).
func fuseHostArgs(volLabel string) []string {
	// One argv per -o so a volname with spaces is never a separate FUSE token.
	args := []string{"-ofsname=ClassicStack"}
	if volLabel != "" {
		args = append(args, "-ovolname="+escapeFuseOpt(volLabel))
	}
	if n := fuseIOSize(); n > 0 {
		args = append(args, fmt.Sprintf("-oiosize=%d", n))
	}
	return args
}

// fuseIOSize is the macFUSE I/O block size for this host. 0 means omit the
// option (Linux libfuse has no iosize). AFP over EtherTalk is a slow link, so
// Darwin uses the documented platform minimum rather than the 64 KiB default.
func fuseIOSize() int {
	if runtime.GOOS != "darwin" {
		return 0
	}
	if runtime.GOARCH == "arm64" {
		return fuseIOSizeDarwinARM64
	}
	return fuseIOSizeDarwinIntel
}

// escapeFuseOpt encodes a FUSE -o value. fuse_opt splits options on comma and
// unescapes \NNN octal, so a space becomes \040 and a comma/backslash is
// backslash-escaped — matching fuse_opt_add_opt_escaped plus fstab whitespace.
func escapeFuseOpt(s string) string {
	if !strings.ContainsAny(s, " ,\\\t") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			b.WriteString(`\\`)
		case ',':
			b.WriteString(`\,`)
		case ' ':
			b.WriteString(`\040`)
		case '\t':
			b.WriteString(`\011`)
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
