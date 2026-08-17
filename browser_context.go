package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type paletteState struct {
	Type string `json:"type"`
	Chin []struct {
		Profile        string `json:"profile"`
		Focused        bool   `json:"focused"`
		ActiveTabTitle string `json:"activeTabTitle"`
		ActiveTabURL   string `json:"activeTabUrl"`
	} `json:"chin"`
}

func activeBrowserHost(ctx context.Context, target windowInfo) string {
	profile, ok := strings.CutPrefix(target.AppID, "browser-")
	if !ok || profile == "" {
		return ""
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return ""
	}
	dialer := net.Dialer{Timeout: 150 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "unix", filepath.Join(runtimeDir, "palette-ui.sock"))
	if err != nil {
		return ""
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
	var state paletteState
	if json.NewDecoder(io.LimitReader(conn, 1<<20)).Decode(&state) != nil || state.Type != "state" {
		return ""
	}
	for _, window := range state.Chin {
		if window.Profile != profile || !window.Focused || window.ActiveTabTitle == "" || !strings.Contains(target.Title, window.ActiveTabTitle) {
			continue
		}
		u, err := url.Parse(window.ActiveTabURL)
		if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			return strings.ToLower(u.Hostname())
		}
	}
	return ""
}
