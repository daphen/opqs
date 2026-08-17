package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestActiveBrowserHostMatchesTargetProfileAndTitle(t *testing.T) {
	dir := t.TempDir()
	old := os.Getenv("XDG_RUNTIME_DIR")
	if err := os.Setenv("XDG_RUNTIME_DIR", dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv("XDG_RUNTIME_DIR", old) })
	listener, err := net.Listen("unix", filepath.Join(dir, "palette-ui.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte(`{"type":"state","chin":[{"profile":"work","focused":true,"activeTabTitle":"Other","activeTabUrl":"https://wrong.test"},{"profile":"personal","focused":true,"activeTabTitle":"Formula 1®","activeTabUrl":"https://account.formula1.com/login?token=not-retained"}]}`))
	}()
	target := windowInfo{AppID: "browser-personal", Title: "Formula 1® - Helium"}
	if got := activeBrowserHost(context.Background(), target); got != "account.formula1.com" {
		t.Fatalf("got %q", got)
	}
}

func TestActiveBrowserHostFailsClosedForMismatchedWindow(t *testing.T) {
	dir := t.TempDir()
	old := os.Getenv("XDG_RUNTIME_DIR")
	if err := os.Setenv("XDG_RUNTIME_DIR", dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv("XDG_RUNTIME_DIR", old) })
	listener, err := net.Listen("unix", filepath.Join(dir, "palette-ui.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte(`{"type":"state","chin":[{"profile":"personal","focused":true,"activeTabTitle":"Different tab","activeTabUrl":"https://wrong.test"}]}`))
	}()
	if got := activeBrowserHost(context.Background(), windowInfo{AppID: "browser-personal", Title: "Formula 1® - Helium"}); got != "" {
		t.Fatalf("unexpected host %q", got)
	}
}
