package main

import (
	"io"
	"net"
	"os"
	"strings"
	"testing"
)

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
