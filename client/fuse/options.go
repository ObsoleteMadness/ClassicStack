package fuse

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
