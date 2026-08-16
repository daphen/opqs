package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSafeTemplateFieldsDropsUnlabelledConcealedFields(t *testing.T) {
	var tpl templateItem
	if err := json.Unmarshal([]byte(`{"fields":[{"id":"u","type":"STRING","purpose":"USERNAME","label":"username"},{"id":"p","type":"CONCEALED","purpose":"PASSWORD","label":"password"},{"id":"x","type":"CONCEALED","label":"private answer"}]}`), &tpl); err != nil {
		t.Fatal(err)
	}
	got := safeTemplateFields(tpl)
	if len(got) != 2 || got[0].Kind != "username" || got[1].Kind != "password" {
		t.Fatalf("unsafe or missing fields: %#v", got)
	}
}

func TestSearchSuggestsItemAndFieldWithoutValues(t *testing.T) {
	s := metadataStore{items: []itemMetadata{{
		ID: "item", Title: "GitHub", Username: "me@example.test", VaultID: "vault", Vault: "Personal", Category: "LOGIN",
		Fields: ensureCommonFields(nil, "LOGIN"),
	}}}
	got := s.search("github email", 10)
	if len(got) == 0 || got[0].FieldKind != "username" {
		t.Fatalf("expected username suggestion: %#v", got)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "value") || strings.Contains(string(raw), "secret") {
		t.Fatalf("secret-capable field leaked into protocol: %s", raw)
	}
}

func TestFuzzyScoreRequiresEveryToken(t *testing.T) {
	if _, ok := fuzzyScore("github otp", "github one time password otp"); !ok {
		t.Fatal("expected match")
	}
	if _, ok := fuzzyScore("github missing", "github password"); ok {
		t.Fatal("unexpected match")
	}
}
