package config

import (
	"errors"
	"strings"
	"time"
)

// FUSEKey is the well-known section key for host FUSE/WinFsp mount config.
const FUSEKey = "FUSE"

// FUSEVolumesKey is the repeated-section schema key for auto-mounted FUSE volumes.
const FUSEVolumesKey = "FUSEVolumes"

// DefaultFUSEMountTimeoutSeconds is how long a FUSE mount waits to connect to
// the remote server when [FUSE] mount_timeout_seconds is unset or zero.
const DefaultFUSEMountTimeoutSeconds = 30

// FUSESection is the host-mount config: connect timeout for FUSE/WinFsp mounts.
// Auto-mounted volumes live in the repeated FUSEVolumes list. It is a well-known
// Model field (like Client/HTTP), not a registered component.
type FUSESection struct {
	// MountTimeoutSeconds is how long to wait to connect to a remote server when
	// mounting. 0 = DefaultFUSEMountTimeoutSeconds.
	MountTimeoutSeconds int `toml:"mount_timeout_seconds,omitempty" display:"Mount timeout (seconds)" desc:"How long to wait to connect to a remote server when mounting a volume. 0 = 30." example:"30"`
}

// Key returns the well-known section key.
func (FUSESection) Key() string { return FUSEKey }

// Clone returns a copy.
func (s FUSESection) Clone() FUSESection { return s }

// DefaultFUSE is the product default: 30-second connect timeout.
func DefaultFUSE() FUSESection {
	return FUSESection{MountTimeoutSeconds: DefaultFUSEMountTimeoutSeconds}
}

// MountTimeout is the connect deadline, applying DefaultFUSEMountTimeoutSeconds
// when MountTimeoutSeconds is unset or negative.
func (s FUSESection) MountTimeout() time.Duration {
	n := s.MountTimeoutSeconds
	if n <= 0 {
		n = DefaultFUSEMountTimeoutSeconds
	}
	return time.Duration(n) * time.Second
}

// Validate checks the timeout.
func (s FUSESection) Validate() error {
	if s.MountTimeoutSeconds < 0 {
		return errors.New("fuse: mount_timeout_seconds must be >= 0")
	}
	return nil
}

// ApplyFUSEDefaults fills in the 30-second timeout when [FUSE] is omitted, or
// when a present table left the timeout unset.
func ApplyFUSEDefaults(s FUSESection, sectionPresent bool) FUSESection {
	if !sectionPresent {
		return DefaultFUSE()
	}
	if s.MountTimeoutSeconds <= 0 {
		s.MountTimeoutSeconds = DefaultFUSEMountTimeoutSeconds
	}
	return s
}

// FUSEVolumeSection is one auto-mounted remote volume: a client URI, a host
// mountpoint, and an optional read-only flag. It is a NamedSection; the instance
// name is the mountpoint.
type FUSEVolumeSection struct {
	// Remote is the client URI of the share to mount, e.g.
	// smb://user:pass@host,smb/share or afp://server/Volume.
	Remote string `toml:"remote" display:"Remote path" desc:"Client URI of the share (scheme://[user[:pass]@]server[,transport]/volume)." example:"smb://user:pass@foohost,smb/share"`
	// Mountpoint is the host path (or Windows drive letter) to attach at.
	Mountpoint string `toml:"mountpoint" display:"Local mount point" desc:"Host directory or Windows drive letter to mount on." example:"/Volumes/share" widget:"path"`
	// ReadOnly mounts the volume read-only even if the remote share is writable.
	ReadOnly bool `toml:"read_only,omitempty" display:"Read-only" desc:"Mount the volume read-only."`
}

var (
	_ Section      = (*FUSEVolumeSection)(nil)
	_ NamedSection = (*FUSEVolumeSection)(nil)
	_ SecretMasker = (*FUSEVolumeSection)(nil)
)

// Key returns the shared repeated-section schema key.
func (s *FUSEVolumeSection) Key() string { return FUSEVolumesKey }

// InstanceName returns the per-volume instance name (the host mountpoint).
func (s *FUSEVolumeSection) InstanceName() string { return strings.TrimSpace(s.Mountpoint) }

// Clone returns a deep copy.
func (s *FUSEVolumeSection) Clone() Section {
	cp := *s
	return &cp
}

// MaskedClone returns a deep copy with a password in Remote redacted.
func (s *FUSEVolumeSection) MaskedClone() Section {
	cp := s.Clone().(*FUSEVolumeSection)
	cp.Remote = maskRemotePassword(cp.Remote)
	return cp
}

// Unmask restores a redacted Remote password from prev.
func (s *FUSEVolumeSection) Unmask(prev Section) Section {
	cp := s.Clone().(*FUSEVolumeSection)
	var prior string
	if pv, ok := prev.(*FUSEVolumeSection); ok {
		prior = pv.Remote
	}
	cp.Remote = unmaskRemotePassword(cp.Remote, prior)
	return cp
}

// Validate requires a remote URI and a mountpoint.
func (s *FUSEVolumeSection) Validate() error {
	if strings.TrimSpace(s.Remote) == "" {
		return errors.New("fuse volume: remote path is required")
	}
	if strings.TrimSpace(s.Mountpoint) == "" {
		return errors.New("fuse volume: mountpoint is required")
	}
	return nil
}

// FUSEVolumesFromModel returns configured auto-mount volume sections in
// registration order, or nil when none.
func FUSEVolumesFromModel(m *Model) []*FUSEVolumeSection {
	if m == nil {
		return nil
	}
	list := m.List(FUSEVolumesKey)
	out := make([]*FUSEVolumeSection, 0, len(list))
	for _, sec := range list {
		if vs, ok := sec.(*FUSEVolumeSection); ok {
			out = append(out, vs)
		}
	}
	return out
}

// RegisterFUSEVolumes installs the auto-mount volume repeated-section schema so
// codecs round-trip each volume. Called from the compose client registry wiring
// so a build without the in-process client excludes the section.
func RegisterFUSEVolumes() {
	Register(SectionSchema{
		Key:      FUSEVolumesKey,
		Repeated: true,
		New:      func() Section { return &FUSEVolumeSection{} },
		Validate: func(s Section) error {
			if vs, ok := s.(*FUSEVolumeSection); ok {
				return vs.Validate()
			}
			return nil
		},
		DisplayName: "FUSE auto-mounted volumes",
		Description: "Remote shares mounted on the host at startup (URI, mountpoint, read-only).",
	})
}

// maskRemotePassword replaces a URI userinfo password with RedactedSecret.
// scheme://user:pass@host → scheme://user:********@host. A URI with no password
// is returned unchanged (so the UI can tell "no password" from "password hidden").
func maskRemotePassword(remote string) string {
	user, pass, host, ok := splitRemoteUserinfo(remote)
	if !ok || pass == "" {
		return remote
	}
	scheme, _, _ := strings.Cut(remote, "://")
	return scheme + "://" + user + ":" + RedactedSecret + "@" + host
}

// unmaskRemotePassword restores a redacted URI password from prior. A Remote
// whose password is not the sentinel is kept verbatim (the operator changed it).
func unmaskRemotePassword(remote, prior string) string {
	_, pass, _, ok := splitRemoteUserinfo(remote)
	if !ok || pass != RedactedSecret {
		return remote
	}
	pUser, pPass, _, pOK := splitRemoteUserinfo(prior)
	if !pOK || pPass == "" {
		// No stored password to restore — drop the sentinel rather than persist it.
		scheme, rest, cutOK := strings.Cut(remote, "://")
		if !cutOK {
			return remote
		}
		at := strings.LastIndex(rest, "@")
		if at < 0 {
			return remote
		}
		user, _, _ := strings.Cut(rest[:at], ":")
		return scheme + "://" + user + "@" + rest[at+1:]
	}
	scheme, _, _ := strings.Cut(remote, "://")
	_, _, host, _ := splitRemoteUserinfo(remote)
	return scheme + "://" + pUser + ":" + pPass + "@" + host
}

// splitRemoteUserinfo pulls user, password, and the remainder (server[,transport]/path)
// from a client URI. ok is false when the input has no scheme:// or no userinfo '@'.
func splitRemoteUserinfo(remote string) (user, pass, rest string, ok bool) {
	_, after, cutOK := strings.Cut(remote, "://")
	if !cutOK {
		return "", "", "", false
	}
	at := strings.LastIndex(after, "@")
	if at < 0 {
		return "", "", "", false
	}
	creds, host := after[:at], after[at+1:]
	if u, p, hasColon := strings.Cut(creds, ":"); hasColon {
		return u, p, host, true
	}
	return creds, "", host, true
}
