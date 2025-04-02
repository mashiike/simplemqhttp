package simplemqhttp

import "net"

type Addr string

var _ net.Addr = Addr("")

// Network returns the network type of the address.
func (a Addr) Network() string {
	return "SakuraCloud SimpleMQ"
}

// String returns the string representation of the address.
func (a Addr) String() string {
	return string(a)
}
