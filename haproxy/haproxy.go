package haproxy

import (
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
)

// RunCommand sends args (joined by spaces) as a single command to conn, copies
// the reply to stdout, then returns.
func RunCommand(conn net.Conn, args []string) error {
	cmd := strings.Join(args, " ")
	if _, err := fmt.Fprintf(conn, "%s\n", cmd); err != nil {
		return fmt.Errorf("sending command: %w", err)
	}
	if _, err := io.Copy(os.Stdout, conn); err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	return nil
}

// QuerySocket sends a command to conn and returns the full response as a string.
// HAProxy closes the connection after the response in non-interactive mode, so
// io.ReadAll naturally terminates at EOF.
func QuerySocket(conn net.Conn, args []string) (string, error) {
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

// ServerEntry holds the parsed fields we care about from "show servers state".
type ServerEntry struct {
	Backend    string
	Server     string
	OpState    int
	AdminState int
	Weight     int
}

// ParseServersState parses the output of "show servers state" into a slice of
// ServerEntry values.  The first line is the format version, comment lines
// start with '#', and each data line is space-separated with at least 8 fields.
func ParseServersState(data string) ([]ServerEntry, error) {
	var entries []ServerEntry
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
		entries = append(entries, ServerEntry{
			Backend:    fields[1],
			Server:     fields[3],
			OpState:    opState,
			AdminState: adminState,
			Weight:     weight,
		})
	}
	return entries, nil
}

// ServerStatus converts the operational and admin state integers returned by
// "show servers state" into a human-readable status string.
//
// Admin-state bit meanings (HAProxy source):
//
//	0x01 SRV_ADMF_FMAINT  – forced maintenance
//	0x02 SRV_ADMF_IMAINT  – inherited maintenance
//	0x04 SRV_ADMF_CMAINT  – configuration maintenance
//	0x08 SRV_ADMF_FDRAIN  – forced drain
//	0x10 SRV_ADMF_IDRAIN  – inherited drain
func ServerStatus(opState, adminState int) string {
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

// RunHealth prints a table of backend/server, status, and weight for every
// server returned by "show servers state".  If filter is non-empty, only rows
// whose "backend/server" column contains filter as a substring are printed.
// If showHeader is false, the header row is suppressed.
func RunHealth(conn net.Conn, filter string, showHeader bool) error {
	data, err := QuerySocket(conn, []string{"show", "servers", "state"})
	if err != nil {
		return err
	}
	entries, err := ParseServersState(data)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if showHeader {
		fmt.Fprintln(w, "# BACKEND/SERVER\tSTATUS\tWEIGHT")
	}
	for _, e := range entries {
		name := e.Backend + "/" + e.Server
		if filter != "" && !strings.Contains(name, filter) {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%d\n", name, ServerStatus(e.OpState, e.AdminState), e.Weight)
	}
	return w.Flush()
}

// RunList prints every backend/server pair, one per line.
func RunList(conn net.Conn) error {
	data, err := QuerySocket(conn, []string{"show", "servers", "state"})
	if err != nil {
		return err
	}
	entries, err := ParseServersState(data)
	if err != nil {
		return err
	}
	for _, e := range entries {
		fmt.Fprintf(os.Stdout, "%s/%s\n", e.Backend, e.Server)
	}
	return nil
}

// RunListBackends prints each backend name returned by "show backend".
func RunListBackends(conn net.Conn) error {
	data, err := QuerySocket(conn, []string{"show", "backend"})
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

// RunListServers prints each unique server name found in "show servers state",
// preserving first-seen order.
func RunListServers(conn net.Conn) error {
	data, err := QuerySocket(conn, []string{"show", "servers", "state"})
	if err != nil {
		return err
	}
	entries, err := ParseServersState(data)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, e := range entries {
		if !seen[e.Server] {
			seen[e.Server] = true
			fmt.Fprintln(os.Stdout, e.Server)
		}
	}
	return nil
}
