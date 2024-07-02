package ip

import (
	"net"
)

// IP Our reference type.
type IP struct {
	IPv4 net.IP
	IPv6 net.IP
}

// IsIPv6Available Check if the IPv6 is available.
// https://stackoverflow.com/a/48519490/4949938
func (i *IP) IsIPv6Available() bool {
	return i.IPv6.To4() != nil
}
