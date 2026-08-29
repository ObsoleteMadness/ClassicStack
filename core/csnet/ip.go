package csnet

import (
	"errors"
	"strconv"
)

// ErrBadIPv4 reports a string ParseIPv4 could not parse as a dotted-quad IPv4
// address.
var ErrBadIPv4 = errors.New("csnet: invalid IPv4 address")

// IPv4 is a 4-byte IPv4 address in network byte order.
type IPv4 [4]byte

// String renders ip as a dotted-quad, e.g. "10.0.0.1".
func (ip IPv4) String() string {
	return strconv.Itoa(int(ip[0])) + "." + strconv.Itoa(int(ip[1])) + "." +
		strconv.Itoa(int(ip[2])) + "." + strconv.Itoa(int(ip[3]))
}
