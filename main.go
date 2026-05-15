package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/pflag"
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

// querySocket sends a command to conn and returns the full response as a string.
// HAProxy closes the connection after the response in non-interactive mode, so
// io.ReadAll naturally terminates at EOF.
func querySocket(conn net.Conn, args []string) (string, error) {
	cmd := strings.Join(args, " ")
	if _, err := fmt.Fprintf(conn, "%s\n", cmd); err != nil {
		return "", fmt.Errorf("sending command: %w", err)
	}
	data, err := io.ReadAll(conn)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	return string(data), nil
}

// serverEntry holds the parsed fields we care about from "show servers state".
type serverEntry struct {
	backend    string
	server     string
	opState    int
	adminState int
	weight     int
}

// parseServersState parses the output of "show servers state" into a slice of
// serverEntry values.  The first line is the format version, comment lines
// start with '#', and each data line is space-separated with at least 8 fields.
func parseServersState(data string) ([]serverEntry, error) {
	var entries []serverEntry
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Skip the format-version line (a bare integer).
		if _, err := strconv.Atoi(line); err == nil {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		opState, _ := strconv.Atoi(fields[5])
		adminState, _ := strconv.Atoi(fields[6])
		weight, _ := strconv.Atoi(fields[7])
		entries = append(entries, serverEntry{
			backend:    fields[1],
			server:     fields[3],
			opState:    opState,
			adminState: adminState,
			weight:     weight,
		})
	}
	return entries, nil
}

// serverStatus converts the operational and admin state integers returned by
// "show servers state" into a human-readable status string.
//
// Admin-state bit meanings (HAProxy source):
//
//	0x01 SRV_ADMF_FMAINT  – forced maintenance
//	0x02 SRV_ADMF_IMAINT  – inherited maintenance
//	0x04 SRV_ADMF_CMAINT  – configuration maintenance
//	0x08 SRV_ADMF_FDRAIN  – forced drain
//	0x10 SRV_ADMF_IDRAIN  – inherited drain
func serverStatus(opState, adminState int) string {
	if adminState&0x07 != 0 {
		return "MAINT"
	}
	if adminState&0x18 != 0 {
		return "DRAIN"
	}
	switch opState {
	case 0:
		return "DOWN"
	case 1:
		return "STARTING"
	case 2:
		return "UP"
	case 3:
		return "STOPPING"
	default:
		return "UNKNOWN"
	}
}

// runHealth prints a table of backend/server, status, and weight for every
// server returned by "show servers state".  If filter is non-empty, only rows
// whose "backend/server" column contains filter as a substring are printed.
// If showHeader is false, the header row is suppressed.
func runHealth(conn net.Conn, filter string, showHeader bool) error {
	data, err := querySocket(conn, []string{"show", "servers", "state"})
	if err != nil {
		return err
	}
	entries, err := parseServersState(data)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if showHeader {
		fmt.Fprintln(w, "# BACKEND/SERVER\tSTATUS\tWEIGHT")
	}
	for _, e := range entries {
		name := e.backend + "/" + e.server
		if filter != "" && !strings.Contains(name, filter) {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%d\n", name, serverStatus(e.opState, e.adminState), e.weight)
	}
	return w.Flush()
}

// runList prints every backend/server pair, one per line.
func runList(conn net.Conn) error {
	data, err := querySocket(conn, []string{"show", "servers", "state"})
	if err != nil {
		return err
	}
	entries, err := parseServersState(data)
	if err != nil {
		return err
	}
	for _, e := range entries {
		fmt.Fprintf(os.Stdout, "%s/%s\n", e.backend, e.server)
	}
	return nil
}

// runListBackends prints each backend name returned by "show backend".
func runListBackends(conn net.Conn) error {
	data, err := querySocket(conn, []string{"show", "backend"})
	if err != nil {
		return err
	}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fmt.Fprintln(os.Stdout, line)
	}
	return nil
}

// runListServers prints each unique server name found in "show servers state",
// preserving first-seen order.
func runListServers(conn net.Conn) error {
	data, err := querySocket(conn, []string{"show", "servers", "state"})
	if err != nil {
		return err
	}
	entries, err := parseServersState(data)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, e := range entries {
		if !seen[e.server] {
			seen[e.server] = true
			fmt.Fprintln(os.Stdout, e.server)
		}
	}
	return nil
}

func main() {
	var socket string
	var noHeader bool
	pflag.CommandLine.SortFlags = false
	pflag.StringVarP(&socket, "socket", "s", defaultSocket, "HAProxy API socket")
	pflag.BoolVarP(&noHeader, "no-header", "H", false, "don't print headers")
	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [-s socket] [-H] [<command>]\n\n", pflag.CommandLine.Name())
		fmt.Fprintf(os.Stderr, "flags:\n")
		pflag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nhactl commands:\n")
		fmt.Fprintf(os.Stderr, "  health [filter]       show server health, optionally filter output\n")
		fmt.Fprintf(os.Stderr, "  list                  list backend/server pairs\n")
		fmt.Fprintf(os.Stderr, "  list backends         list backends\n")
		fmt.Fprintf(os.Stderr, "  list servers          list servers\n\n")
		fmt.Fprintf(os.Stderr, "HAProxy API commands:\n")
		fmt.Fprintf(os.Stderr, "  help [<command>]      list matching or all commands\n\n")
	}
	pflag.Parse()

	args := pflag.Args()

	conn, err := dialSocket(socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error connecting to %s: %v\n", socket, err)
		os.Exit(1)
	}
	defer conn.Close()

	if len(args) > 0 {
		var err error
		switch args[0] {
		case "health":
			filter := ""
			if len(args) > 1 {
				filter = args[1]
			}
			err = runHealth(conn, filter, !noHeader)
		case "list", "ls":
			if len(args) > 1 {
				switch args[1] {
				case "backends", "backend", "be":
					err = runListBackends(conn)
				case "servers", "server", "srv":
					err = runListServers(conn)
				default:
					fmt.Fprintf(os.Stderr, "unknown list subcommand: %s\n", args[1])
					os.Exit(1)
				}
			} else {
				err = runList(conn)
			}
		default:
			err = runCommand(conn, args)
		}
		if err != nil {
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
