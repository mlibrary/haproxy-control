package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

const defaultSocket = "/run/haproxy/admin.sock"

// dialSocket connects to the given socket address. If the address contains a
// colon it is treated as a TCP host:port pair; otherwise it is treated as a
// Unix socket path.
func dialSocket(socket string) (net.Conn, error) {
	if strings.Contains(socket, ":") {
		return net.Dial("tcp", socket)
	}
	return net.Dial("unix", socket)
}

// runCommand sends args (joined by spaces) as a single command to conn, copies
// the reply to stdout, then returns.
func runCommand(conn net.Conn, args []string) error {
	cmd := strings.Join(args, " ")
	if _, err := fmt.Fprintf(conn, "%s\n", cmd); err != nil {
		return fmt.Errorf("sending command: %w", err)
	}
	if _, err := io.Copy(os.Stdout, conn); err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	return nil
}

// runInteractive enables the HAProxy interactive prompt and relays lines
// typed on stdin to the socket, printing all socket output to stdout.
func runInteractive(conn net.Conn) error {
	if _, err := fmt.Fprintf(conn, "prompt timed\n"); err != nil {
		return fmt.Errorf("sending prompt command: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(os.Stdout, conn)
		done <- err
	}()

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if _, err := fmt.Fprintf(conn, "%s\n", scanner.Text()); err != nil {
			break
		}
	}
	conn.Close()
	return <-done
}

func main() {
	var socket string
	flag.StringVar(&socket, "socket", defaultSocket, "socket to connect to (unix path or host:port)")
	flag.StringVar(&socket, "s", defaultSocket, "socket to connect to (unix path or host:port) (shorthand)")
	flag.Parse()

	args := flag.Args()

	conn, err := dialSocket(socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error connecting to %s: %v\n", socket, err)
		os.Exit(1)
	}
	defer conn.Close()

	if len(args) > 0 {
		if err := runCommand(conn, args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := runInteractive(conn); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}
