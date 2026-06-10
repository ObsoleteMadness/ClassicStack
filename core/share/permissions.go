package share

// Permissions is the per-share access policy. It is a deliberate stub: the
// project is a compatibility server and performs no access control yet (a share
// is reachable by anything that can connect, see the AFP service security
// posture). The type exists so the share descriptor and config model can carry a
// permissions field ahead of real enforcement, which lands later without a
// signature change here.
//
// When enforcement arrives this will gain fields (allow/deny lists, guest
// policy, per-user rights) and the file services will consult it at
// session/tree-connect time.
type Permissions struct{}

// AllowsGuest reports whether unauthenticated access is permitted. With no policy
// configured the answer is yes — the current world-readable default.
func (Permissions) AllowsGuest() bool { return true }
