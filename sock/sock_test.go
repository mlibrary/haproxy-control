package sock_test

import (
	"net"
	"testing"

	"hactl/sock"
)

func TestDialSocket_UnixPath(t *testing.T) {
	// Create a temporary Unix socket listener so DialSocket can actually connect.
	ln, err := net.Listen("unix", "/tmp/haproxy_test.sock")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	conn, err := sock.DialSocket("/tmp/haproxy_test.sock")
	if err != nil {
		t.Fatalf("DialSocket unix: %v", err)
	}
	conn.Close()
}

func TestDialSocket_TCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	conn, err := sock.DialSocket(ln.Addr().String())
	if err != nil {
		t.Fatalf("DialSocket tcp: %v", err)
	}
	conn.Close()
}
