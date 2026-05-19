package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/pflag"

	"hactl/haproxy"
	"hactl/sock"
)

// Version is set at build time via -ldflags "-X main.Version=<value>"
var Version = "DEV"

const defaultSocket = "/run/haproxy/admin.sock"

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

	quit := false
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if _, err := fmt.Fprintf(conn, "%s\n", line); err != nil {
			break
		}
		if strings.TrimSpace(line) == "quit" {
			quit = true
			break
		}
	}
	if !quit {
		fmt.Fprintln(os.Stdout)
		conn.Close()
		<-done
		return nil
	}
	return <-done
}

func main() {
	var socket string
	var noHeader bool
	pflag.CommandLine.SortFlags = false
	pflag.StringVarP(&socket, "socket", "s", defaultSocket, "HAProxy API socket")
	pflag.BoolVarP(&noHeader, "no-header", "H", false, "don't print headers")
	pflag.Usage = func() {
		w := os.Stderr
		identity := func(s string) string { return s }
		b, u := identity, identity
		if isatty.IsTerminal(w.Fd()) && os.Getenv("NO_COLOR") == "" {
			b = func(s string) string { return "\x1b[1m" + s + "\x1b[0m" }
			u = func(s string) string { return "\x1b[4m" + s + "\x1b[0m" }
		}

		fmt.Fprint(w, b("usage:")+" hactl [-s "+u("socket")+"] [-H] ["+u("<command>")+"]\n\n")
		fmt.Fprint(w, b("flags:\n"))
		pflag.PrintDefaults()
		fmt.Fprint(w, "\n")
		fmt.Fprint(w, b("hactl commands:\n"))
		fmt.Fprint(w, "  health ["+u("filter")+"]       show server health, optionally filter output\n")
		fmt.Fprint(w, "  list                  list backend/server pairs\n")
		fmt.Fprint(w, "  list backends         list backends\n")
		fmt.Fprint(w, "  list servers          list servers\n\n")
		fmt.Fprint(w, b("HAProxy API commands:\n"))
		fmt.Fprint(w, "  prompt                start interactive command shell\n")
		fmt.Fprint(w, "  help ["+u("<command>")+"]      list matching or all commands\n\n")
	}
	pflag.Parse()

	args := pflag.Args()

	if len(args) == 0 {
		fmt.Fprintf(os.Stdout, "hactl %s - CLI for HAProxy Runtime API\n\n", Version)
		pflag.Usage()
		return
	}

	conn, err := sock.DialSocket(socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error connecting to %s: %v\n", socket, err)
		os.Exit(1)
	}
	defer conn.Close()

	{
		var err error
		switch args[0] {
		case "health":
			filter := ""
			if len(args) > 1 {
				filter = args[1]
			}
			err = haproxy.RunHealth(conn, filter, !noHeader)
		case "list", "ls":
			if len(args) > 1 {
				switch args[1] {
				case "backends", "backend", "be":
					err = haproxy.RunListBackends(conn)
				case "servers", "server", "srv":
					err = haproxy.RunListServers(conn)
				default:
					fmt.Fprintf(os.Stderr, "unknown subcommand: %s %s\n", args[0], args[1])
					os.Exit(1)
				}
			} else {
				err = haproxy.RunList(conn)
			}
		case "prompt", "shell":
			if err := runInteractive(conn); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
		default:
			err = haproxy.RunCommand(conn, args)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}
