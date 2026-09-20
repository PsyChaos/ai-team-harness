// Package registry discovers CLI availability without scheduling or invoking work.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Budget preserves unknown values as null, independently of explicit zero limits.
// Units and period must be declared by the operator; no price/quota is inferred.
type Budget struct {
	Amount *float64 `json:"amount"`
	Unit   string   `json:"unit"`
	Period string   `json:"period"`
}

type Model struct {
	ID         string   `json:"id,omitempty"`
	CLIDefault bool     `json:"cli_default"`
	Enabled    bool     `json:"enabled"`
	Efforts    []string `json:"efforts"`
	WorkTypes  []string `json:"work_types"`
	Budget     *Budget  `json:"budget"`
}

func (m Model) Label() string {
	if m.CLIDefault {
		return "CLI default"
	}
	return m.ID
}

type Provider struct {
	Name        string  `json:"name"`
	CLI         string  `json:"cli"`
	Enabled     bool    `json:"enabled"`
	Concurrency int     `json:"concurrency"`
	Budget      *Budget `json:"budget"`
	Models      []Model `json:"models"`
}

type Config struct {
	Providers []Provider `json:"providers"`
}

// Load accepts explicit operator assertions about capabilities, not model guesses.
func Load(r io.Reader) (Config, error) {
	var c Config
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("expected one config document")
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	seen := map[string]bool{}
	for _, p := range c.Providers {
		if (p.Name != "codex" && p.Name != "claude" && p.Name != "gemini") || seen[p.Name] {
			return fmt.Errorf("unsupported or duplicate provider %q", p.Name)
		}
		seen[p.Name] = true
		if p.CLI == "" || p.Concurrency < 0 {
			return fmt.Errorf("invalid CLI or concurrency for %s", p.Name)
		}
		if err := validateBudget(p.Budget); err != nil {
			return err
		}
		models := map[string]bool{}
		for _, m := range p.Models {
			if m.CLIDefault == (m.ID != "") {
				return fmt.Errorf("%s model requires either id or cli_default", p.Name)
			}
			key := m.ID
			if m.CLIDefault {
				key = "CLI default"
			}
			if models[key] {
				return fmt.Errorf("duplicate model %q", key)
			}
			models[key] = true
			if err := validateBudget(m.Budget); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateBudget(b *Budget) error {
	if b != nil && (b.Unit == "" || b.Period == "" || (b.Amount != nil && *b.Amount < 0)) {
		return fmt.Errorf("budget requires unit, period and nonnegative amount when known")
	}
	return nil
}

type Evidence struct {
	DiscoveredAt time.Time `json:"discovered_at"`
	Source       string    `json:"source"`
}

type ModelEntry struct {
	Model
	Label     string   `json:"label"`
	Available bool     `json:"available"`
	Evidence  Evidence `json:"evidence"`
}

type Entry struct {
	Provider  Provider     `json:"provider"`
	Installed bool         `json:"installed"`
	Version   string       `json:"version"`
	Auth      string       `json:"auth"` // authenticated, unauthenticated, or unknown
	Available bool         `json:"available"`
	Reason    string       `json:"reason,omitempty"`
	Evidence  Evidence     `json:"evidence"`
	Models    []ModelEntry `json:"models"`
}

// Discover only executes version/auth status commands. Each command has a deadline.
// Raw auth output is deliberately never returned: it can include account details.
// Gemini remains unavailable until an auth probe is verified for its CLI version.
func Discover(ctx context.Context, c Config, source string) ([]Entry, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(c.Providers))
	for _, p := range c.Providers {
		e := Entry{Provider: p, Auth: "unknown", Evidence: Evidence{time.Now().UTC(), source + "; PATH lookup"}}
		path, err := exec.LookPath(p.CLI)
		if err != nil {
			e.Reason = "CLI not installed or not executable"
		} else {
			e.Installed = true
			e.Evidence.Source += "; " + path + " --version"
			out, err := probe(ctx, path, "--version")
			prefix := "codex-cli "
			if p.Name == "claude" {
				prefix = ""
			}
			version := strings.TrimSpace(string(out))
			validVersion := (p.Name == "codex" && strings.HasPrefix(version, prefix)) || (p.Name == "claude" && strings.HasSuffix(version, " (Claude Code)"))
			if err != nil || !validVersion {
				e.Reason = "version probe failed or unsupported CLI"
			} else {
				e.Version = version
				switch p.Name {
				case "codex":
					e.Evidence.Source += "; login status"
					out, err = probe(ctx, path, "login", "status")
					status := strings.TrimSpace(string(out))
					if err == nil && loggedIn(status) {
						e.Auth = "authenticated"
					} else if strings.Contains(status, "Not logged in") {
						e.Auth = "unauthenticated"
					}
				case "claude":
					e.Evidence.Source += "; auth status --json"
					out, err = probe(ctx, path, "auth", "status", "--json")
					var status struct {
						LoggedIn *bool `json:"loggedIn"`
					}
					if json.Unmarshal(out, &status) == nil && status.LoggedIn != nil {
						if *status.LoggedIn && err == nil {
							e.Auth = "authenticated"
						} else if !*status.LoggedIn {
							e.Auth = "unauthenticated"
						}
					}
				}
				if e.Auth != "authenticated" {
					e.Reason = "authentication " + e.Auth
				}
			}
		}
		e.Available = p.Enabled && e.Version != "" && e.Auth == "authenticated"
		if !p.Enabled {
			e.Reason = "provider disabled"
		}
		for _, m := range p.Models {
			e.Models = append(e.Models, ModelEntry{m, m.Label(), e.Available && m.Enabled, Evidence{e.Evidence.DiscoveredAt, source + "; explicit model configuration"}})
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func probe(ctx context.Context, path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = time.Second
	// Codex writes login status to stderr, Claude writes JSON to stdout.
	if len(args) == 1 && args[0] == "--version" {
		return cmd.Output()
	}
	return cmd.CombinedOutput()
}

// Ignore CLI startup warnings without accepting arbitrary successful output.
func loggedIn(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Logged in using ") {
			return true
		}
	}
	return false
}
