package haproxy_test

import (
	"io"
	"net"
	"os"
	"strings"
	"testing"

	"hactl/haproxy"
)

// sampleServersState is a minimal "show servers state" reply used across tests.
const sampleServersState = `1
# be_id be_name srv_id srv_name srv_addr srv_op_state srv_admin_state srv_uweight srv_iweight srv_time_since_last_change srv_check_status srv_check_result srv_check_health srv_check_state srv_agent_state bkd_f_forced_id srv_f_forced_id srv_fqdn srv_port srvrecord srv_use_ssl srv_check_port srv_check_addr srv_agent_addr srv_agent_port
1 web 1 web1 192.168.1.1 2 0 100 100 10 6 3 4 6 0 0 0 - 80 - 0 0 - - 0
1 web 2 web2 192.168.1.2 0 0 50 50 5 1 0 0 0 0 0 0 - 80 - 0 0 - - 0
2 api 1 api1 10.0.0.1 2 1 100 100 3 6 3 4 6 0 0 0 - 8080 - 0 0 - - 0
2 api 2 web1 10.0.0.2 2 0 80 80 1 6 3 4 6 0 0 0 - 8080 - 0 0 - - 0
`

// captureStdout redirects os.Stdout to a pipe, runs f, then returns the
// captured output and restores os.Stdout.
func captureStdout(f func()) string {
	r, w, _ := os.Pipe()
	orig := os.Stdout
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = orig
	out, _ := io.ReadAll(r)
	return string(out)
}

// mockConn starts a fake HAProxy server on a net.Pipe that responds to one
// command with the given reply string, then closes.  It returns the client
// side of the pipe.
func mockConn(t *testing.T, wantCmd, reply string) net.Conn {
	t.Helper()
	server, client := net.Pipe()
	go func() {
		defer server.Close()
		buf := make([]byte, 1024)
		n, _ := server.Read(buf)
		got := strings.TrimSpace(string(buf[:n]))
		if got != wantCmd {
			t.Errorf("server got %q, want %q", got, wantCmd)
		}
		server.Write([]byte(reply))
	}()
	return client
}

func TestParseServersState(t *testing.T) {
	entries, err := haproxy.ParseServersState(sampleServersState)
	if err != nil {
		t.Fatalf("ParseServersState: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("got %d entries, want 4", len(entries))
	}
	tests := []struct {
		backend, server string
		opState         int
		adminState      int
		weight          int
	}{
		{"web", "web1", 2, 0, 100},
		{"web", "web2", 0, 0, 50},
		{"api", "api1", 2, 1, 100},
		{"api", "web1", 2, 0, 80},
	}
	for i, tc := range tests {
		e := entries[i]
		if e.Backend != tc.backend || e.Server != tc.server ||
			e.OpState != tc.opState || e.AdminState != tc.adminState ||
			e.Weight != tc.weight {
			t.Errorf("entry[%d] = %+v, want %+v", i, e, tc)
		}
	}
}

func TestServerStatus(t *testing.T) {
	tests := []struct {
		opState    int
		adminState int
		want       string
	}{
		{2, 0, "UP"},
		{0, 0, "DOWN"},
		{1, 0, "STARTING"},
		{3, 0, "STOPPING"},
		{2, 1, "MAINT"}, // FMAINT
		{2, 2, "MAINT"}, // IMAINT
		{2, 4, "MAINT"}, // CMAINT
		{2, 8, "DRAIN"}, // FDRAIN
		{2, 16, "DRAIN"}, // IDRAIN
		{99, 0, "UNKNOWN"},
	}
	for _, tc := range tests {
		got := haproxy.ServerStatus(tc.opState, tc.adminState)
		if got != tc.want {
			t.Errorf("ServerStatus(%d,%d) = %q, want %q", tc.opState, tc.adminState, got, tc.want)
		}
	}
}

func TestRunHealth_NoFilter(t *testing.T) {
	client := mockConn(t, "show servers state", sampleServersState)
	out := captureStdout(func() {
		if err := haproxy.RunHealth(client, "", true); err != nil {
			t.Fatalf("RunHealth: %v", err)
		}
	})
	for _, want := range []string{"# BACKEND/SERVER", "web/web1", "web/web2", "api/api1", "api/web1", "UP", "DOWN", "MAINT"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunHealth_WithFilter(t *testing.T) {
	client := mockConn(t, "show servers state", sampleServersState)
	out := captureStdout(func() {
		if err := haproxy.RunHealth(client, "web/", true); err != nil {
			t.Fatalf("RunHealth: %v", err)
		}
	})
	if !strings.Contains(out, "web/web1") || !strings.Contains(out, "web/web2") {
		t.Errorf("expected web rows, got:\n%s", out)
	}
	if strings.Contains(out, "api/") {
		t.Errorf("api rows should be filtered out, got:\n%s", out)
	}
}

func TestRunHealth_NoHeader(t *testing.T) {
	client := mockConn(t, "show servers state", sampleServersState)
	out := captureStdout(func() {
		if err := haproxy.RunHealth(client, "", false); err != nil {
			t.Fatalf("RunHealth: %v", err)
		}
	})
	if strings.Contains(out, "BACKEND/SERVER") {
		t.Errorf("header should be suppressed with showHeader=false, got:\n%s", out)
	}
	if !strings.Contains(out, "web/web1") {
		t.Errorf("data rows should still appear, got:\n%s", out)
	}
}

func TestRunList(t *testing.T) {
	client := mockConn(t, "show servers state", sampleServersState)
	out := captureStdout(func() {
		if err := haproxy.RunList(client); err != nil {
			t.Fatalf("RunList: %v", err)
		}
	})
	for _, want := range []string{"web/web1", "web/web2", "api/api1", "api/web1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunListBackends(t *testing.T) {
	reply := "# name\nweb\napi\n"
	client := mockConn(t, "show backend", reply)
	out := captureStdout(func() {
		if err := haproxy.RunListBackends(client); err != nil {
			t.Fatalf("RunListBackends: %v", err)
		}
	})
	if !strings.Contains(out, "web") || !strings.Contains(out, "api") {
		t.Errorf("expected backends in output, got:\n%s", out)
	}
	if strings.Contains(out, "#") {
		t.Errorf("comment line should be suppressed, got:\n%s", out)
	}
}

func TestRunListServers_UniqueOnly(t *testing.T) {
	client := mockConn(t, "show servers state", sampleServersState)
	out := captureStdout(func() {
		if err := haproxy.RunListServers(client); err != nil {
			t.Fatalf("RunListServers: %v", err)
		}
	})
	// "web1" appears in both web and api backends; must appear only once.
	count := strings.Count(out, "web1")
	if count != 1 {
		t.Errorf("web1 appeared %d times, want 1:\n%s", count, out)
	}
	for _, want := range []string{"web1", "web2", "api1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunCommand(t *testing.T) {
	server, client := net.Pipe()

	// Server: read the command and reply.
	go func() {
		defer server.Close()
		buf := make([]byte, 64)
		n, _ := server.Read(buf)
		got := strings.TrimSpace(string(buf[:n]))
		if got != "show info" {
			t.Errorf("server got %q, want %q", got, "show info")
		}
		server.Write([]byte("Name: HAProxy\n"))
	}()

	// Redirect stdout so we can capture output.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w

	runErr := haproxy.RunCommand(client, []string{"show", "info"})

	w.Close()
	os.Stdout = origStdout
	out, _ := io.ReadAll(r)

	if runErr != nil {
		t.Fatalf("RunCommand: %v", runErr)
	}
	if !strings.Contains(string(out), "HAProxy") {
		t.Errorf("output %q does not contain expected text", string(out))
	}
}
