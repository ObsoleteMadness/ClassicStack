//go:build afp || all

package app

import "github.com/ObsoleteMadness/ClassicStack/service/afp"

// registeredFSTypes returns the AFP filesystem types registered in this build
// (local_fs, plus macgarden when built with that tag). Used by the management
// plane to populate the volume/share FS-type dropdown.
func registeredFSTypes() []string {
	return afp.RegisteredFSTypes()
}
