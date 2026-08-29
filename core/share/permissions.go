package share

import "strings"

// Permissions is the per-share access policy: a coarse gate naming which
// authenticated users may see and bind the share. It is NOT file-level ACLs and
// NOT a per-user read-only flag (ReadOnly stays share-wide) — matching the
// compatibility-server posture (gate the share, not the filesystem).
//
// The empty value is the historical world-readable default: no allow-list means
// guest/anonymous access is permitted, so a server that defines no users behaves
// exactly as before this field existed. The file services consult Allows at
// login-time enumeration (which shares are listed) and at tree-connect / OpenVol
// (which shares may be bound).
type Permissions struct {
	// AllowedUsers is the set of usernames permitted to use this share. Empty
	// means guest/anonymous (world) access. Matching is case-insensitive.
	AllowedUsers []string
}

// AllowsGuest reports whether unauthenticated (guest/anonymous) access is
// permitted. With no allow-list configured the answer is yes — the world-readable
// default.
func (p Permissions) AllowsGuest() bool { return len(p.AllowedUsers) == 0 }

// Allows reports whether the given identity may use the share. A guest-open share
// (empty allow-list) admits anyone, including an empty (guest) username. A
// restricted share admits only a non-empty username present in the allow-list
// (case-insensitive). Guests are therefore refused a restricted share.
func (p Permissions) Allows(username string) bool {
	if len(p.AllowedUsers) == 0 {
		return true
	}
	if username == "" {
		return false
	}
	for _, u := range p.AllowedUsers {
		if strings.EqualFold(u, username) {
			return true
		}
	}
	return false
}
