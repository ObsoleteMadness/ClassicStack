//go:build !afp && !all

package app

import "errors"

// validateExtMap is unavailable in builds without AFP; the extension map is an
// AFP-only concept, so editing it has no meaning here.
func validateExtMap([]byte) error {
	return errors.New("extension map editing requires an AFP-enabled build")
}
