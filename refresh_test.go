package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDaemonRefreshesOnlyOnDemand(t *testing.T) {
	if os.Getenv("OPQS_TEST_DAEMON") == "1" {
		if err := runDaemon(); err != nil {
			t.Fatal(err)
		}
		return
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	unlocked := filepath.Join(dir, "unlocked")
	for name, script := range map[string]string{
		"op":   fmt.Sprintf("#!/bin/sh\necho called >> %q\n[ -f %q ] || exit 1\nprintf '[]\\n'\n", calls, unlocked),
		"niri": fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '{\"id\":1,\"pid\":%d,\"app_id\":\"editor\",\"title\":\"Editor\"}'\n", os.Getpid()),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDaemonRefreshesOnlyOnDemand$")
	cmd.Env = append(os.Environ(), "OPQS_TEST_DAEMON=1", "XDG_RUNTIME_DIR="+dir, "PATH="+dir+":"+os.Getenv("PATH"))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	var conn net.Conn
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		conn, err = net.Dial("unix", filepath.Join(dir, "opqs.sock"))
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if conn == nil {
		t.Fatal("daemon socket did not become ready")
	}
	defer conn.Close()
	encoder, decoder := json.NewEncoder(conn), json.NewDecoder(conn)
	send := func(msg wireMessage) {
		t.Helper()
		if err := encoder.Encode(msg); err != nil {
			t.Fatal(err)
		}
	}
	read := func(kind, status string) wireEvent {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			var event wireEvent
			if err := decoder.Decode(&event); err != nil {
				t.Fatal(err)
			}
			if event.Type == kind && (status == "" || event.Status == status) {
				return event
			}
		}
	}
	assertCalls := func(want int) {
		t.Helper()
		raw, err := os.ReadFile(calls)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if got := strings.Count(string(raw), "called\n"); got != want {
			t.Fatalf("1Password calls = %d, want %d", got, want)
		}
	}
	send(wireMessage{Type: "hello"})
	send(wireMessage{Type: "refresh", Nonce: "invalid"})
	time.Sleep(200 * time.Millisecond)
	assertCalls(0)

	send(wireMessage{Type: "summon"})
	read("ack", "ready")
	shown := read("show", "")
	read("status", "stale")
	assertCalls(1)
	time.Sleep(200 * time.Millisecond)
	assertCalls(1)

	if err := os.WriteFile(unlocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	send(wireMessage{Type: "refresh", Nonce: shown.Nonce})
	read("status", "ready")
	read("results", "")
	assertCalls(2)

	send(wireMessage{Type: "cancel", Nonce: shown.Nonce})
	send(wireMessage{Type: "refresh", Nonce: shown.Nonce})
	time.Sleep(200 * time.Millisecond)
	assertCalls(2)

	send(wireMessage{Type: "summon"})
	read("ack", "ready")
	read("show", "")
	read("status", "ready")
	read("results", "")
	assertCalls(3)
}
