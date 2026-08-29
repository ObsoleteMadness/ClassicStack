//go:build tinygo

package browse

import "net"

func osInterfaceIPv4(_ string) net.IP {
	return nil
}
