package fs

import "errors"

// fork_native.go registers "native" as a per-OS ALIAS for the host's own fork layout,
// resolved at build time by nativeForkTarget (fork_native_<goos>.go):
//
//	Windows → "ads"   (NTFS alternate data streams, SFM layout — fork_ads.go)
//	darwin  → "hfs"   (HFS+ "..namedfork/rsrc" + com.apple.FinderInfo — adapter/fork/hfs)
//	Linux   → "xattr" (Netatalk user.* extended attributes — fork_xattr.go)
//	other   → ""      (no host-native layout; "native" is unavailable)
//
// "native" is the PRESENTATION of a file's forks in the host's own idiom. On a plain host
// directory (local_fs) that means storing them there — ads streams on NTFS, etc. On a
// client mount of a native-fork protocol (AFP), the remote volume already HAS the forks;
// the ads/hfs/xattr engine then represents those wire forks in the host idiom — e.g. the
// ads engine surfaces the AFP resource fork as the NTFS ":AFP_Resource" stream. Either
// way the engine reaches the actual fork through the base's native ForkEngine when the
// base has one (see fork_ads.go's base-ForkEngine delegation), NOT by inventing a
// "name:AFP_Resource" path — so "native" and "ads" both yield real SFM streams over an
// AFP mount, while "appledouble" would instead project ._name sidecars.
//
// So `fork_backend = "native"` means "present forks the way this host natively does"
// without naming a platform. The concrete engines keep their own names too, so a share
// can pin one explicitly. The ads/xattr engines live in core/fs and are always linked;
// hfs lives in adapter/fork/hfs (host syscalls) and must be blank-imported on darwin.
//
// This replaces the former "native" host adapter + its forknative build tag / disabled
// stub: there is no tag any more.

// ErrNativeForkUnsupported is returned when "native" is selected on a platform that has
// no host-native fork layout wired.
var ErrNativeForkUnsupported = errors.New("fs: native fork backend has no host layout on this platform")

func init() {
	RegisterForkAdapter("native", func(spec ShareSpec, base FileSystem) (ForkEngine, error) {
		target := nativeForkTarget
		if target == "" {
			return nil, ErrNativeForkUnsupported
		}
		return forkAdapterByName(target, spec, base)
	})
}
