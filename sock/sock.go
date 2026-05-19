package sock

import (
	"net"
	"strings"
)

// DialSocket connects to the given socket address. If the address contains a
// colon it is treated as a TCP host:port pair; otherwise it is treated as a
// Unix socket path.
func DialSocket(socket string) (net.Conn, error) {
	if strings.Contains(socket, ":") {
		return net.Dial("tcp", socket)
	}
	return net.Dial("unix", socket)
}
