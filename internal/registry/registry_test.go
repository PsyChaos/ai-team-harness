package registry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name, body, auth, version string
		available                 bool
	}{
		{"codex", `case "$*" in
 --version) echo 'codex-cli 0.154.0';;
 'login status') echo 'WARNING: could not create PATH aliases' >&2; echo 'Logged in using ChatGPT' >&2;;
 *) exit 9;; esac`, "authenticated", "codex-cli 0.154.0", true},
		{"claude", `case "$*" in
 --version) echo '2.1.235 (Claude Code)';;
 'auth status --json') echo '{"loggedIn":true}';;
 *) exit 9;; esac`, "authenticated", "2.1.235 (Claude Code)", true},
		{"codex", `case "$*" in
 --version) echo 'codex-cli 0.154.0';;
 'login status') echo 'Not logged in' >&2; exit 1;;
 *) exit 9;; esac`, "unauthenticated", "codex-cli 0.154.0", false},
		{"claude", `case "$*" in
 --version) echo '2.1.235 (Claude Code)';;
 'auth status --json') echo '{"loggedIn":false}'; exit 1;;
 *) exit 9;; esac`, "unauthenticated", "2.1.235 (Claude Code)", false},
		{"claude", `case "$*" in
 --version) echo '2.1.235 (Claude Code)';;
 *) echo '{"loggedIn":true}'; exit 1;; esac`, "unknown", "2.1.235 (Claude Code)", false},
		{"codex", `case "$*" in
 --version) echo 'codex-cli 0.154.0';;
 *) echo 'unrecognized status';; esac`, "unknown", "codex-cli 0.154.0", false},
	} {
		t.Run(tc.name+"/"+tc.auth, func(t *testing.T) {
			c := Config{[]Provider{{Name: tc.name, CLI: fixture(t, tc.name, tc.body), Enabled: true, Concurrency: 1, Models: []Model{{CLIDefault: true, Enabled: true}, {ID: "configured-model", Enabled: false}}}}}
			entries, err := Discover(context.Background(), c, "fixture config")
			if err != nil {
				t.Fatal(err)
			}
			e := entries[0]
			if !e.Installed || e.Auth != tc.auth || e.Version != tc.version || e.Available != tc.available {
				t.Fatalf("unexpected discovery: %+v", e)
			}
			if e.Models[0].Label != "CLI default" || e.Models[0].ID != "" || e.Models[0].Available != tc.available || e.Models[1].Available {
				t.Fatalf("unexpected models: %+v", e.Models)
			}
			if e.Evidence.DiscoveredAt.IsZero() || !strings.Contains(e.Evidence.Source, "fixture config") || e.Models[0].Evidence.Source == "" {
				t.Fatal("missing provenance")
			}
			c.Providers[0].Enabled = false
			entries, err = Discover(context.Background(), c, "fixture")
			if err != nil || entries[0].Available || entries[0].Models[0].Available {
				t.Fatal("disabled provider selectable")
			}
		})
	}
}

func TestMissingCLIAndUnknownBudget(t *testing.T) {
	c, err := Load(strings.NewReader(`{"providers":[{"name":"gemini","cli":"/nonexistent/registry-fixture-gemini","enabled":true,"concurrency":1,"models":[{"cli_default":true,"enabled":true}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := Discover(context.Background(), c, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	e := entries[0]
	if e.Installed || e.Available || e.Models[0].Available || e.Auth != "unknown" {
		t.Fatalf("missing CLI available: %+v", e)
	}
	if e.Provider.Budget != nil || e.Models[0].Budget != nil {
		t.Fatal("unknown budget must remain nil")
	}
	data, _ := json.Marshal(e)
	if !strings.Contains(string(data), `"budget":null`) {
		t.Fatal(string(data))
	}
	var b Budget
	if err := json.Unmarshal([]byte(`{"amount":0,"unit":"USD","period":"day"}`), &b); err != nil {
		t.Fatal(err)
	}
	if b.Amount == nil || *b.Amount != 0 {
		t.Fatal("explicit zero lost")
	}
}

func TestConfigValidation(t *testing.T) {
	for _, input := range []string{
		`{"providers":[],"typo":true}`, `{"providers":[]} {}`,
		`{"providers":[{"name":"other","cli":"other"}]}`,
		`{"providers":[{"name":"codex","cli":"codex","models":[{"id":"invented","cli_default":true}]}]}`,
		`{"providers":[{"name":"codex","cli":"codex","models":[{}]}]}`,
		`{"providers":[{"name":"codex","cli":"codex","concurrency":-1}]}`,
		`{"providers":[{"name":"codex","cli":"codex","budget":{"amount":-1,"unit":"USD","period":"day"}}]}`,
	} {
		if _, err := Load(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	c, err := Load(strings.NewReader(`{"providers":[{"name":"codex","cli":"codex","models":[{"id":"operator-model","enabled":true,"efforts":["operator-effort"],"work_types":["review"]}]}]}`))
	if err != nil || c.Providers[0].Models[0].Efforts[0] != "operator-effort" {
		t.Fatalf("explicit capabilities lost: %+v %v", c, err)
	}
}

func TestCancelledProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := Config{Providers: []Provider{{Name: "codex", CLI: fixture(t, "codex", "echo 'codex-cli 0.154.0'"), Enabled: true}}}
	entries, err := Discover(ctx, c, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Available || entries[0].Auth != "unknown" {
		t.Fatal("cancelled probe must fail closed")
	}
}
