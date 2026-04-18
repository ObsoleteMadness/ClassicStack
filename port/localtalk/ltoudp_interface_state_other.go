//go:build !windows

package localtalk

func multicastInterfaceOperStatus() (map[uint32]bool, error) {
	return nil, nil
}
