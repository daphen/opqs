package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	maxSecretBytes = 64 << 10
	typeDelayMS    = 5
)

type windowInfo struct {
	ID          uint64 `json:"id"`
	Title       string `json:"title"`
	AppID       string `json:"app_id"`
	PID         int    `json:"pid"`
	WorkspaceID uint64 `json:"workspace_id"`
	IsFocused   bool   `json:"is_focused"`
	StartTime   string `json:"-"`
}

type fieldRequest struct {
	Kind  string
	Label string
}

type cappedBuffer struct {
	Data []byte
	Max  int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Max <= 0 {
		b.Max = maxSecretBytes
	}
	if len(b.Data)+len(p) > b.Max {
		return 0, errors.New("output exceeds limit")
	}
	b.Data = append(b.Data, p...)
	return len(p), nil
}
func (b *cappedBuffer) Bytes() []byte { return b.Data }
func (b *cappedBuffer) Zero() {
	for i := range b.Data {
		b.Data[i] = 0
	}
	b.Data = nil
}

func safeCommandEnv() []string {
	keep := map[string]bool{"HOME": true, "PATH": true, "XDG_RUNTIME_DIR": true, "XDG_CONFIG_HOME": true, "WAYLAND_DISPLAY": true, "NIRI_SOCKET": true, "DBUS_SESSION_BUS_ADDRESS": true, "LANG": true, "LC_ALL": true}
	out := []string{"OP_DEBUG=false", "OP_CACHE=false", "NO_COLOR=1"}
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && keep[key] {
			out = append(out, entry)
		}
	}
	return out
}

func captureFocusedWindow(ctx context.Context) (windowInfo, error) {
	cmd := exec.CommandContext(ctx, "niri", "msg", "--json", "focused-window")
	cmd.Env = safeCommandEnv()
	var out cappedBuffer
	out.Max = 128 << 10
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return windowInfo{}, errors.New("cannot read focused window")
	}
	var w windowInfo
	if err := json.Unmarshal(out.Bytes(), &w); err != nil || w.ID == 0 || w.PID <= 0 {
		return windowInfo{}, errors.New("no focused target")
	}
	if strings.EqualFold(w.Title, "opqs") || strings.Contains(strings.ToLower(w.Title), "credential picker") {
		return windowInfo{}, errors.New("picker cannot target itself")
	}
	start, err := processStartTime(w.PID)
	if err != nil {
		return windowInfo{}, errors.New("target process disappeared")
	}
	w.StartTime = start
	return w, nil
}

func processStartTime(pid int) (string, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	end := bytes.LastIndexByte(raw, ')')
	if end < 0 {
		return "", errors.New("invalid process stat")
	}
	parts := strings.Fields(string(raw[end+1:]))
	if len(parts) < 20 {
		return "", errors.New("invalid process stat")
	}
	return parts[19], nil
}

func sameWindow(a, b windowInfo) bool {
	if a.ID != b.ID || a.PID != b.PID || a.AppID != b.AppID || a.WorkspaceID != b.WorkspaceID {
		return false
	}
	start, err := processStartTime(b.PID)
	return err == nil && start == a.StartTime
}

func queryFocusedWindow(ctx context.Context) (windowInfo, error) {
	cmd := exec.CommandContext(ctx, "niri", "msg", "--json", "focused-window")
	cmd.Env = safeCommandEnv()
	var out cappedBuffer
	out.Max = 128 << 10
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return windowInfo{}, err
	}
	var w windowInfo
	if err := json.Unmarshal(out.Bytes(), &w); err != nil {
		return windowInfo{}, err
	}
	return w, nil
}

func focusAndValidate(ctx context.Context, target windowInfo) error {
	cmd := exec.CommandContext(ctx, "niri", "msg", "action", "focus-window", "--id", strconv.FormatUint(target.ID, 10))
	cmd.Env = safeCommandEnv()
	if err := cmd.Run(); err != nil {
		return errors.New("target window is unavailable")
	}
	stable := 0
	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) {
		w, err := queryFocusedWindow(ctx)
		if err == nil && w.IsFocused && sameWindow(target, w) {
			stable++
		} else {
			stable = 0
		}
		if stable >= 2 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Millisecond):
		}
	}
	return errors.New("target focus could not be verified")
}

func retrieveField(ctx context.Context, itemID, vaultID string, req fieldRequest) (*cappedBuffer, error) {
	label := strings.TrimSpace(req.Label)
	if itemID == "" || vaultID == "" || label == "" {
		return nil, errors.New("invalid item or field")
	}
	reference := "op://" + url.PathEscape(vaultID) + "/" + url.PathEscape(itemID) + "/" + url.PathEscape(label)
	if req.Kind == "otp" {
		reference += "?attribute=otp"
	}
	cmd := exec.CommandContext(ctx, "op", "read", "--no-newline", "--cache=false", reference)
	cmd.Env = safeCommandEnv()
	buf := &cappedBuffer{Max: maxSecretBytes}
	cmd.Stdout = buf
	if err := cmd.Run(); err != nil {
		buf.Zero()
		return nil, errors.New("field could not be retrieved; unlock 1Password and try again")
	}
	if len(buf.Data) == 0 {
		buf.Zero()
		return nil, errors.New("selected field is empty")
	}
	return buf, nil
}

func typeSecret(ctx context.Context, secret []byte) error {
	cmd := exec.CommandContext(ctx, "wtype", "-d", strconv.Itoa(typeDelayMS), "-")
	cmd.Env = safeCommandEnv()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return errors.New("typing unavailable")
	}
	if err := cmd.Start(); err != nil {
		return errors.New("typing unavailable")
	}
	_, writeErr := io.Copy(stdin, bytes.NewReader(secret))
	closeErr := stdin.Close()
	waitErr := cmd.Wait()
	if writeErr != nil || closeErr != nil || waitErr != nil {
		return errors.New("typing failed")
	}
	return nil
}

func inject(ctx context.Context, target windowInfo, itemID, vaultID string, req fieldRequest) error {
	if err := focusAndValidate(ctx, target); err != nil {
		return err
	}
	secret, err := retrieveField(ctx, itemID, vaultID, req)
	if err != nil {
		return err
	}
	defer secret.Zero()
	focused, err := queryFocusedWindow(ctx)
	if err != nil || !focused.IsFocused || !sameWindow(target, focused) {
		return errors.New("focus changed before typing; nothing was typed")
	}
	if err := typeSecret(ctx, secret.Bytes()); err != nil {
		return err
	}
	focused, err = queryFocusedWindow(ctx)
	if err != nil || !focused.IsFocused || !sameWindow(target, focused) {
		return errors.New("focus changed while typing; review the target")
	}
	return nil
}
