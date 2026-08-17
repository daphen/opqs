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

func TestRetrieveFieldKeepsSecretOutOfArguments(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	writeExecutable(t, dir, "op", fmt.Sprintf("printf '%%s\\n' \"$@\" >'%s'\nprintf 'sentinel'\n", argsFile))
	withPath(t, dir)
	buf, err := retrieveField(context.Background(), "item-id", "vault-id", fieldRequest{Kind: "password", Label: "password"})
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Zero()
	if string(buf.Bytes()) != "sentinel" {
		t.Fatalf("got %q", buf.Bytes())
	}
	args, _ := os.ReadFile(argsFile)
	if bytes.Contains(args, buf.Bytes()) {
		t.Fatal("secret appeared in op arguments")
	}
	if !bytes.Contains(args, []byte("op://vault-id/item-id/password")) {
		t.Fatalf("missing narrow reference: %s", args)
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

func TestPasteSecretUsesSensitiveClipboardWithoutProcessLeak(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, dir, "wl-copy", fmt.Sprintf(`
if [ "$1" = "--clear" ]; then
  : >'%s/cleared'
  exit 0
fi
tr '\000' '\n' </proc/$$/cmdline >'%s/copy-cmdline'
tr '\000' '\n' </proc/$$/environ >'%s/copy-environ'
cat >'%s/copied'
exec sleep 10
`, dir, dir, dir, dir))
	writeExecutable(t, dir, "wtype", fmt.Sprintf("printf '%%s\\n' \"$@\" >'%s/wtype-args'\n", dir))
	withPath(t, dir)
	secret := []byte("PROC_LEAK_SENTINEL_93f2")
	if err := pasteSecret(context.Background(), secret); err != nil {
		t.Fatal(err)
	}
	copied, _ := os.ReadFile(filepath.Join(dir, "copied"))
	cmdline, _ := os.ReadFile(filepath.Join(dir, "copy-cmdline"))
	environ, _ := os.ReadFile(filepath.Join(dir, "copy-environ"))
	wtypeArgs, _ := os.ReadFile(filepath.Join(dir, "wtype-args"))
	if !bytes.Equal(copied, secret) {
		t.Fatalf("clipboard stdin got %q", copied)
	}
	if bytes.Contains(cmdline, secret) || bytes.Contains(environ, secret) || bytes.Contains(wtypeArgs, secret) {
		t.Fatal("secret appeared in /proc argv or environment")
	}
	if !bytes.Contains(cmdline, []byte("--sensitive")) {
		t.Fatalf("sensitive clipboard hint missing: %q", cmdline)
	}
	if string(wtypeArgs) != "-M\nctrl\n-k\nv\n-m\nctrl\n" {
		t.Fatalf("unexpected paste chord: %q", wtypeArgs)
	}
	if _, err := os.Stat(filepath.Join(dir, "cleared")); err != nil {
		t.Fatal("clipboard was not cleared")
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
