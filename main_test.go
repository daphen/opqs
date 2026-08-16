package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestValidateFieldRestrictsStandardAndCustomSelections(t *testing.T) {
	item := itemMetadata{Fields: []field{{Kind: "username", Label: "username"}, {Kind: "password", Label: "password"}}}
	if got, ok := validateField(item, "username", "username"); !ok || got.Label != "username" {
		t.Fatalf("standard field rejected: %#v", got)
	}
	if _, ok := validateField(item, "password", "username"); ok {
		t.Fatal("mismatched standard field accepted")
	}
	if _, ok := validateField(item, "custom", "recovery code"); !ok {
		t.Fatal("custom label rejected")
	}
	if _, ok := validateField(item, "custom", "bad\nlabel"); ok {
		t.Fatal("control characters accepted")
	}
}

func TestWireEventsCannotCarrySecretValues(t *testing.T) {
	typ := wireEvent{Type: "show", Nonce: "nonce", Target: "Editor", Suggestions: []suggestion{{ItemID: "item", FieldKind: "password", FieldLabel: "password"}}}
	raw, err := json.Marshal(typ)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"value"`, `"secret"`, `"password_value"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("secret-capable protocol key found: %s", raw)
		}
	}
}

func TestSessionNonceAndExpiry(t *testing.T) {
	s := &server{clients: make(map[*client]struct{}), session: &activeSession{Nonce: "right", Expires: time.Now().Add(time.Second)}}
	if !s.validNonce("right") || s.validNonce("wrong") {
		t.Fatal("nonce validation failed")
	}
	s.session.Expires = time.Now().Add(-time.Second)
	if s.validNonce("right") {
		t.Fatal("expired session accepted")
	}
}
