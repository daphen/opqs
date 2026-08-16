package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

type itemMetadata struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Username string   `json:"username,omitempty"`
	VaultID  string   `json:"vault_id"`
	Vault    string   `json:"vault"`
	Category string   `json:"category"`
	URLs     []string `json:"urls,omitempty"`
	Fields   []field  `json:"fields,omitempty"`
}

type field struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Alias string `json:"-"`
}

type suggestion struct {
	ItemID     string  `json:"item_id"`
	Title      string  `json:"title"`
	Username   string  `json:"username,omitempty"`
	VaultID    string  `json:"vault_id"`
	Vault      string  `json:"vault"`
	Category   string  `json:"category"`
	FieldKind  string  `json:"field_kind,omitempty"`
	FieldLabel string  `json:"field_label,omitempty"`
	Fields     []field `json:"fields,omitempty"`
	Label      string  `json:"label"`
	Subtitle   string  `json:"subtitle"`
	Score      int     `json:"-"`
}

type metadataStore struct {
	mu        sync.RWMutex
	items     []itemMetadata
	updatedAt time.Time
}

type listItem struct {
	ID                    string `json:"id"`
	Title                 string `json:"title"`
	Category              string `json:"category"`
	AdditionalInformation string `json:"additional_information"`
	Vault                 struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"vault"`
	URLs []struct {
		Href string `json:"href"`
	} `json:"urls"`
}

type templateItem struct {
	Fields []struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Purpose string `json:"purpose"`
		Label   string `json:"label"`
	} `json:"fields"`
}

func runMetadataCommand(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "op", args...)
	cmd.Env = safeCommandEnv()
	var out cappedBuffer
	out.Max = 8 << 20
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, errors.New("1Password metadata unavailable")
	}
	return out.Bytes(), nil
}

func (s *metadataStore) refresh(ctx context.Context) error {
	raw, err := runMetadataCommand(ctx, "item", "list", "--long", "--format", "json", "--cache=false")
	if err != nil {
		return err
	}
	var listed []listItem
	if err := json.Unmarshal(raw, &listed); err != nil {
		return errors.New("invalid 1Password metadata")
	}
	categories := make(map[string]struct{})
	for _, it := range listed {
		if it.Category != "" {
			categories[it.Category] = struct{}{}
		}
	}
	schemas := make(map[string][]field, len(categories))
	for category := range categories {
		tctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		tplRaw, tplErr := runMetadataCommand(tctx, "item", "template", "get", category, "--format", "json", "--cache=false")
		cancel()
		if tplErr == nil {
			var tpl templateItem
			if json.Unmarshal(tplRaw, &tpl) == nil {
				schemas[category] = safeTemplateFields(tpl)
			}
		}
	}
	items := make([]itemMetadata, 0, len(listed))
	for _, it := range listed {
		if it.ID == "" || it.Title == "" || it.Vault.ID == "" {
			continue
		}
		m := itemMetadata{ID: it.ID, Title: it.Title, Username: it.AdditionalInformation, VaultID: it.Vault.ID, Vault: it.Vault.Name, Category: it.Category}
		for _, u := range it.URLs {
			if u.Href != "" {
				m.URLs = append(m.URLs, u.Href)
			}
		}
		m.Fields = append(m.Fields, schemas[it.Category]...)
		m.Fields = ensureCommonFields(m.Fields, it.Category)
		items = append(items, m)
	}
	s.mu.Lock()
	s.items = items
	s.updatedAt = time.Now()
	s.mu.Unlock()
	return nil
}

func safeTemplateFields(t templateItem) []field {
	out := make([]field, 0, len(t.Fields))
	seen := make(map[string]bool)
	for _, f := range t.Fields {
		label := strings.TrimSpace(f.Label)
		kind := "custom"
		switch strings.ToLower(f.Purpose) {
		case "username":
			kind, label = "username", "username"
		case "password":
			kind, label = "password", "password"
		}
		if strings.EqualFold(f.Type, "OTP") || strings.Contains(strings.ToLower(label), "one-time") {
			kind = "otp"
		}
		if label == "" || seen[strings.ToLower(label)] || strings.EqualFold(f.ID, "notesPlain") || strings.EqualFold(f.Type, "CONCEALED") && kind == "custom" {
			continue
		}
		seen[strings.ToLower(label)] = true
		out = append(out, field{Kind: kind, Label: label, Alias: aliases(kind, label)})
	}
	return out
}

func ensureCommonFields(fields []field, category string) []field {
	seen := make(map[string]bool)
	for _, f := range fields {
		seen[f.Kind] = true
	}
	if category == "LOGIN" || strings.EqualFold(category, "Login") {
		if !seen["username"] {
			fields = append(fields, field{Kind: "username", Label: "username", Alias: aliases("username", "username")})
		}
		if !seen["password"] {
			fields = append(fields, field{Kind: "password", Label: "password", Alias: aliases("password", "password")})
		}
		if !seen["otp"] {
			fields = append(fields, field{Kind: "otp", Label: "one-time password", Alias: aliases("otp", "one-time password")})
		}
	}
	return fields
}

func aliases(kind, label string) string {
	switch kind {
	case "username":
		return label + " username email login user"
	case "password":
		return label + " password pass pwd"
	case "otp":
		return label + " otp totp code one time password"
	default:
		return label + " name email text"
	}
}

func (s *metadataStore) search(query string, limit int) []suggestion {
	s.mu.RLock()
	items := append([]itemMetadata(nil), s.items...)
	s.mu.RUnlock()
	q := strings.ToLower(strings.TrimSpace(query))
	var out []suggestion
	for _, it := range items {
		hosts := make([]string, 0, len(it.URLs))
		for _, raw := range it.URLs {
			if u, err := url.Parse(raw); err == nil {
				hosts = append(hosts, u.Hostname())
			}
		}
		searchParts := []string{it.Title, it.Username, it.Vault, it.Category, strings.Join(hosts, " ")}
		for _, f := range it.Fields {
			searchParts = append(searchParts, f.Alias)
		}
		score, ok := fuzzyScore(q, strings.ToLower(strings.Join(searchParts, " ")))
		if ok {
			out = append(out, suggestion{ItemID: it.ID, Title: it.Title, Username: it.Username, VaultID: it.VaultID, Vault: it.Vault, Category: it.Category, Fields: it.Fields, Label: it.Title, Subtitle: subtitle(it), Score: score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Label < out[j].Label
		}
		return out[i].Score > out[j].Score
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func subtitle(it itemMetadata) string {
	parts := []string{}
	if it.Username != "" {
		parts = append(parts, it.Username)
	}
	if it.Vault != "" {
		parts = append(parts, it.Vault)
	}
	return strings.Join(parts, " · ")
}

func fuzzyScore(query, value string) (int, bool) {
	if query == "" {
		return 0, true
	}
	tokens := strings.Fields(query)
	total := 0
	for _, token := range tokens {
		pos := -1
		last := -2
		score := 0
		for _, r := range token {
			n := strings.IndexRune(value[pos+1:], r)
			if n < 0 {
				return 0, false
			}
			pos += n + 1
			if pos == last+1 {
				score += 8
			} else {
				score += 2
			}
			last = pos
		}
		if strings.Contains(value, token) {
			score += 40
		}
		total += score - pos/8
	}
	return total, true
}
