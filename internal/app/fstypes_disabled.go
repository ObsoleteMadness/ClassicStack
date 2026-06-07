//go:build !afp && !all

package app

// registeredFSTypes returns the default FS-type list when the binary is built
// without AFP. Only the local filesystem backend is meaningful in that case.
func registeredFSTypes() []string {
	return []string{"local_fs"}
}
