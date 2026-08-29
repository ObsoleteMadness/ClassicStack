//go:build afp || smb || ncp || all

package registry

import (
	"github.com/ObsoleteMadness/ClassicStack/adapter/auth/local"
	"github.com/ObsoleteMadness/ClassicStack/core/auth"
	"github.com/ObsoleteMadness/ClassicStack/core/auth/authsection"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
)

// The authentication store is not a standalone component (it has no lifecycle of
// its own); it is a shared object the file services consult. So instead of a
// component Factory, the registry exposes BuildUserStore, which the compose root
// calls once and hands to every enabled file service via SetAuthenticator (an
// Attachable side-effect, like SetBrowseProvider — not a hard dependency).
//
// This file is built whenever a file service is (afp || smb || all); a build with
// neither neither registers the Auth section nor links the store, so it carries no
// auth code at all.

func init() {
	// Register the Auth config section so codecs round-trip it. Kept here (not in
	// an auth-package init) so the section exists exactly when a file service does.
	authsection.Register()
	// Install the user-store constructor into the always-compiled registry hook, so
	// BuildUserStore returns a real store exactly in builds that have a file service
	// (afp||smb||all) and (nil,nil) otherwise.
	userStoreBuilder = buildUserStore
}

// buildUserStore constructs the configured user store from the model's Auth
// section. Only the built-in "local" file-backed store ships today; an unknown
// backend falls back to local (mirroring the registry's "requested but not built"
// handling). The returned store is an auth.UserStore — a full management surface (the
// web UI's user CRUD) and the Authenticator the AFP/SMB login paths consult.
func buildUserStore(m *config.Model) (auth.UserStore, error) {
	sec := authsection.SectionFromModel(m)
	switch sec.EffectiveBackend() {
	case authsection.BackendLocal:
		return local.Open(sec.EffectivePath())
	default:
		// Unknown/unbuilt backend → fall back to the always-present local store.
		return local.Open(sec.EffectivePath())
	}
}
