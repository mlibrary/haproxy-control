package main

import (
	"io"
	"net"
	"os"
	"strings"
	"testing"
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
	entries, err := parseServersState(sampleServersState)
	if err != nil {
		t.Fatalf("parseServersState: %v", err)
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
		if e.backend != tc.backend || e.server != tc.server ||
			e.opState != tc.opState || e.adminState != tc.adminState ||
			e.weight != tc.weight {
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
		got := serverStatus(tc.opState, tc.adminState)
		if got != tc.want {
			t.Errorf("serverStatus(%d,%d) = %q, want %q", tc.opState, tc.adminState, got, tc.want)
		}
	}
}

func TestRunHealth_NoFilter(t *testing.T) {
	client := mockConn(t, "show servers state", sampleServersState)
	out := captureStdout(func() {
		if err := runHealth(client, "", true); err != nil {
			t.Fatalf("runHealth: %v", err)
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
		if err := runHealth(client, "web/", true); err != nil {
			t.Fatalf("runHealth: %v", err)
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
		if err := runHealth(client, "", false); err != nil {
			t.Fatalf("runHealth: %v", err)
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
		if err := runList(client); err != nil {
			t.Fatalf("runList: %v", err)
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
		if err := runListBackends(client); err != nil {
			t.Fatalf("runListBackends: %v", err)
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
		if err := runListServers(client); err != nil {
			t.Fatalf("runListServers: %v", err)
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

// pipeConn wraps a net.Conn so that Close only closes the write side,
// allowing the reader goroutine to drain before the test ends.
// For these tests we use net.Pipe() which gives us synchronous in-memory
// connections.

func TestDialSocket_UnixPath(t *testing.T) {
	// Create a temporary Unix socket listener so dialSocket can actually connect.
	ln, err := net.Listen("unix", "/tmp/haproxy_test.sock")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	conn, err := dialSocket("/tmp/haproxy_test.sock")
	if err != nil {
		t.Fatalf("dialSocket unix: %v", err)
	}
	conn.Close()
}

func TestDialSocket_TCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	conn, err := dialSocket(ln.Addr().String())
	if err != nil {
		t.Fatalf("dialSocket tcp: %v", err)
	}
	conn.Close()
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

	runErr := runCommand(client, []string{"show", "info"})

	w.Close()
	os.Stdout = origStdout
	out, _ := io.ReadAll(r)

	if runErr != nil {
		t.Fatalf("runCommand: %v", runErr)
	}
	if !strings.Contains(string(out), "HAProxy") {
		t.Errorf("output %q does not contain expected text", string(out))
	}
}

func TestRunInteractiveQuit(t *testing.T) {
	server, client := net.Pipe()

	// Server: expect "prompt timed\n", send a prompt, then receive "quit\n"
	// and close. The key is that stdin still has data after "quit" — the
	// function must exit without reading it.
	go func() {
		defer server.Close()
		buf := make([]byte, 128)
		n, _ := server.Read(buf)
		if strings.TrimSpace(string(buf[:n])) != "prompt timed" {
			t.Errorf("expected 'prompt timed', got %q", strings.TrimSpace(string(buf[:n])))
		}
		server.Write([]byte("> "))

		n, _ = server.Read(buf)
		if strings.TrimSpace(string(buf[:n])) != "quit" {
			t.Errorf("expected 'quit', got %q", strings.TrimSpace(string(buf[:n])))
		}
		server.Write([]byte("Bye!\n"))
	}()

	// Provide fake stdin: "quit\n" followed by more data that must NOT be
	// read (if it were, the test would hang because no server goroutine is
	// reading the extra command).
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdinW.WriteString("quit\n")
	stdinW.WriteString("extra line that should never be sent\n")
	stdinW.Close()

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = stdinR
	os.Stdout = stdoutW

	runInteractive(client)

	os.Stdin = origStdin
	stdoutW.Close()
	os.Stdout = origStdout
	stdinR.Close()

	out, _ := io.ReadAll(stdoutR)
	if !strings.Contains(string(out), "Bye!") {
		t.Errorf("output %q does not contain expected text", string(out))
	}
}

func TestRunInteractiveEOF(t *testing.T) {
	server, client := net.Pipe()

	// Server: expect "prompt timed\n", send a prompt, then wait for the
	// client to close the connection (Ctrl-D / stdin EOF path).
	go func() {
		defer server.Close()
		buf := make([]byte, 128)
		n, _ := server.Read(buf)
		if strings.TrimSpace(string(buf[:n])) != "prompt timed" {
			t.Errorf("expected 'prompt timed', got %q", strings.TrimSpace(string(buf[:n])))
		}
		server.Write([]byte("> "))
		// Block until client closes (signals EOF).
		server.Read(buf)
	}()

	// Stdin is closed immediately (simulates Ctrl-D with no input).
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdinW.Close()

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = stdinR
	os.Stdout = stdoutW

	runErr := runInteractive(client)

	os.Stdin = origStdin
	stdoutW.Close()
	os.Stdout = origStdout
	stdinR.Close()

	out, _ := io.ReadAll(stdoutR)

	if runErr != nil {
		t.Errorf("runInteractive returned error on EOF: %v", runErr)
	}
	if len(out) == 0 || out[len(out)-1] != '\n' {
		t.Errorf("expected output to end with newline on EOF, got %q", string(out))
	}
}

func TestRunInteractive(t *testing.T) {
	server, client := net.Pipe()

	serverDone := make(chan struct{})

	// Server: expect "prompt timed\n", exchange one command, then close.
	// Closing the server side will unblock the client's write and end the
	// scanner loop via a broken-pipe error.
	go func() {
		defer close(serverDone)
		defer server.Close()
		buf := make([]byte, 128)
		n, _ := server.Read(buf)
		if strings.TrimSpace(string(buf[:n])) != "prompt timed" {
			t.Errorf("expected 'prompt timed', got %q", strings.TrimSpace(string(buf[:n])))
		}
		server.Write([]byte("> "))

		n, _ = server.Read(buf)
		if strings.TrimSpace(string(buf[:n])) != "show version" {
			t.Errorf("expected 'show version', got %q", strings.TrimSpace(string(buf[:n])))
		}
		server.Write([]byte("HAProxy 2.8\n> "))
		// deferred server.Close() triggers EOF on client reads and a
		// broken-pipe on the next client write, ending the interactive loop.
	}()

	// Provide fake stdin with one command. We leave it open until the server
	// is done so the scanner keeps the loop alive long enough to read the
	// server's reply.
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdinW.WriteString("show version\n")
	go func() {
		<-serverDone
		stdinW.Close()
	}()

	// Capture stdout.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = stdinR
	os.Stdout = stdoutW

	runInteractive(client)

	os.Stdin = origStdin
	stdoutW.Close()
	os.Stdout = origStdout
	stdinR.Close()

	out, _ := io.ReadAll(stdoutR)
	if !strings.Contains(string(out), "HAProxy 2.8") {
		t.Errorf("output %q does not contain expected text", string(out))
	}
}
