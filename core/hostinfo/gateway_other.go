//go:build !windows && !linux && !darwin

package hostinfo

import "net"

// defaultGateway is unsupported on platforms without a per-OS routing-table reader; the
// caller falls back to an explicitly configured gateway.
func defaultGateway() (net.IP, error) {
	return nil, ErrNoDefaultGateway
}
