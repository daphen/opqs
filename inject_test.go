package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeExecutable(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func withPath(t *testing.T, dir string) {
	t.Helper()
	old := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+":"+old); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv("PATH", old) })
}

func TestParseFieldValueRequiresExactlyOneValue(t *testing.T) {
	got, err := parseFieldValue([]byte(`{"id":"password","value":"sentinel"}`))
	if err != nil || string(got) != "sentinel" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := parseFieldValue([]byte(`[{"value":"one"},{"value":"two"}]`)); err == nil {
		t.Fatal("accepted ambiguous fields")
	}
}

func TestCappedBufferZeroesSecret(t *testing.T) {
	b := &cappedBuffer{Max: 16}
	_, _ = b.Write([]byte("sentinel"))
	backing := b.Data
	b.Zero()
	if !bytes.Equal(backing, make([]byte, len(backing))) {
		t.Fatalf("buffer was not overwritten: %q", backing)
	}
}

func TestTypeSecretUsesStdinNotArgvOrEnvironment(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, dir, "wtype", fmt.Sprintf(`
tr '\000' '\n' </proc/$$/cmdline >'%s/cmdline'
tr '\000' '\n' </proc/$$/environ >'%s/environ'
cat >'%s/stdin'
`, dir, dir, dir))
	withPath(t, dir)
	secret := []byte("PROC_LEAK_SENTINEL_93f2")
	if err := typeSecret(context.Background(), secret); err != nil {
		t.Fatal(err)
	}
	stdin, _ := os.ReadFile(filepath.Join(dir, "stdin"))
	cmdline, _ := os.ReadFile(filepath.Join(dir, "cmdline"))
	environ, _ := os.ReadFile(filepath.Join(dir, "environ"))
	if !bytes.Equal(stdin, secret) {
		t.Fatalf("stdin got %q", stdin)
	}
	if bytes.Contains(cmdline, secret) || bytes.Contains(environ, secret) {
		t.Fatal("secret appeared in /proc argv or environment")
	}
}

func TestFocusValidationRejectsChangedWindow(t *testing.T) {
	dir := t.TempDir()
	window := fmt.Sprintf(`{"id":42,"app_id":"other","pid":%d,"workspace_id":3,"is_focused":true}`, os.Getpid())
	writeExecutable(t, dir, "niri", fmt.Sprintf(`
if [ "$1" = "msg" ] && [ "$2" = "action" ]; then exit 0; fi
printf '%%s\n' '%s'
`, window))
	withPath(t, dir)
	start, err := processStartTime(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	target := windowInfo{ID: 41, AppID: "editor", PID: os.Getpid(), WorkspaceID: 3, StartTime: start}
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	err = focusAndValidate(ctx, target)
	if err == nil || !strings.Contains(err.Error(), "verified") {
		t.Fatalf("expected focus rejection, got %v", err)
	}
}
