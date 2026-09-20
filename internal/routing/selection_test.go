package routing

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PsyChaos/ai-team-harness/internal/domain"
	"github.com/PsyChaos/ai-team-harness/internal/registry"
	"github.com/PsyChaos/ai-team-harness/internal/scheduler"
)

type fakeAdapter func(context.Context, []Candidate) (JudgmentResponse, error)

func (f fakeAdapter) Rank(ctx context.Context, c []Candidate) (JudgmentResponse, error) {
	return f(ctx, c)
}
func number(n float64) *float64 { return &n }
func money(n float64) *registry.Budget {
	return &registry.Budget{Amount: number(n), Unit: "USD", Period: "task"}
}
func fixture() (Input, Policy) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	in := Input{Now: now, Providers: map[string]scheduler.Provider{}, Traits: map[string]Traits{"work": {Complexity: "small", Risk: "LOW", WorkType: "code"}}, Observations: map[ObservationKey]Observation{}}
	p := Policy{Version: "test-v1", HealthTTL: time.Minute, Capabilities: map[ModelKey]Capability{}, ProviderOrder: []string{"codex", "claude"}, EffortOrder: map[string][]string{"fast": {"low"}, "balanced": {"medium"}, "strong": {"high"}}, SwitchOnLastAttempt: true, JudgmentTimeout: 20 * time.Millisecond, MinConfidence: 0.75}
	for _, name := range p.ProviderOrder {
		model := registry.Model{ID: name + "-model", Enabled: true, Efforts: []string{"low", "medium", "high"}, WorkTypes: []string{"code", "security"}}
		evidence := registry.Evidence{DiscoveredAt: now, Source: "fake discovery"}
		in.Registry = append(in.Registry, registry.Entry{Provider: registry.Provider{Name: name, CLI: name, Enabled: true, Concurrency: 1, Models: []registry.Model{model}}, Installed: true, Auth: "authenticated", Available: true, Evidence: evidence, Models: []registry.ModelEntry{{Model: model, Available: true, Evidence: evidence}}})
		in.Providers[name] = scheduler.Provider{Healthy: true, Available: 1, ValidUntil: now.Add(time.Minute)}
		p.Capabilities[ModelKey{name, model.ID}] = Capability{Profiles: []string{"fast", "balanced", "strong"}, Roles: []scheduler.Role{scheduler.Implementation}}
	}
	work := scheduler.Work{ID: "work", Issue: domain.Issue{URL: "https://github.com/example/repo/issues/9", State: "OPEN", Repository: domain.Repository{NameWithOwner: "example/repo"}, BlockedBy: domain.Blockers{Complete: true}}, Status: "READY", Role: scheduler.Implementation, MaxAttempts: 2, Providers: p.ProviderOrder}
	in.Frontier = scheduler.New(2).Frontier(scheduler.Snapshot{Now: now, Work: []scheduler.Work{work}, Available: 1, RoleAvailable: map[scheduler.Role]int{scheduler.Implementation: 1}, Providers: in.Providers})
	return in, p
}
func TestRequiredScenarios(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		change                               func(*Input, *Policy)
		provider, profile, effort, exclusion string
	}{
		{"small/simple", nil, "codex", "fast", "low", ""},
		{"high-risk", func(in *Input, _ *Policy) {
			in.Traits["work"] = Traits{Complexity: "small", Risk: "HIGH", WorkType: "code"}
		}, "codex", "strong", "high", ""},
		{"scarce-capacity", func(in *Input, _ *Policy) { h := in.Providers["codex"]; h.Available = 0; in.Providers["codex"] = h }, "claude", "fast", "low", "exhausted capacity"},
		{"disabled-model", func(in *Input, _ *Policy) { in.Registry[0].Models[0].Enabled = false }, "claude", "fast", "low", "disabled"},
		{"unsupported-effort", func(in *Input, _ *Policy) { in.Registry[0].Models[0].Efforts = []string{"ultra"} }, "claude", "fast", "low", "unsupported effort"},
		{"stale-health", func(in *Input, _ *Policy) {
			h := in.Providers["codex"]
			h.ValidUntil = in.Now
			in.Providers["codex"] = h
		}, "claude", "fast", "low", "stale health"},
		{"provider-outage", func(in *Input, _ *Policy) { h := in.Providers["codex"]; h.Healthy = false; in.Providers["codex"] = h }, "claude", "fast", "low", "outage"},
		{"retry/provider-switch", func(in *Input, _ *Policy) {
			in.Frontier[0].Attempts = 1
			v := in.Traits["work"]
			v.PreviousProvider = "codex"
			in.Traits["work"] = v
		}, "claude", "fast", "low", "provider switch"},
		{"stale-registry", func(in *Input, _ *Policy) { in.Registry[0].Evidence.DiscoveredAt = in.Now.Add(-time.Minute) }, "claude", "fast", "low", "stale registry"},
		{"observed-results", func(in *Input, _ *Policy) {
			in.Observations[ObservationKey{ChoiceKey{ModelKey{"claude", "claude-model"}, "low"}, "code"}] = Observation{Successes: 3, ValidUntil: in.Now.Add(time.Minute)}
		}, "claude", "fast", "low", ""},
		{"security-work", func(in *Input, _ *Policy) {
			in.Traits["work"] = Traits{Complexity: "small", Risk: "LOW", WorkType: "security"}
		}, "codex", "strong", "high", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, p := fixture()
			if tc.change != nil {
				tc.change(&in, &p)
			}
			got, err := Select(context.Background(), in, p, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := Tuple{"work", in.Frontier[0].Issue.URL, scheduler.Implementation, tc.provider, tc.provider + "-model", tc.effort, tc.profile}
			if got.Selected == nil || got.Selected.Tuple != want || got.Source != "rules_fallback" || got.PolicyVersion != p.Version || got.Reason == "" {
				t.Fatalf("result: %+v", got)
			}
			if tc.exclusion != "" {
				b, _ := json.Marshal(got.Excluded)
				if !strings.Contains(string(b), tc.exclusion) {
					t.Fatalf("missing exclusion %s: %s", tc.exclusion, b)
				}
			}
			in.Registry[0], in.Registry[1] = in.Registry[1], in.Registry[0]
			again, err := Select(context.Background(), in, p, nil)
			if err != nil || !reflect.DeepEqual(got, again) {
				t.Fatalf("input ordering changed decision: %+v %v", again, err)
			}
		})
	}
}
func TestBudgetAndQuota(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cost   *registry.Budget
		quota  *float64
		limit  *registry.Budget
		reason string
	}{
		{"exceeding", money(6), number(1), money(5), "budget exceeded"},
		{"unknown-cost", nil, nil, money(5), "unknown cost"},
		{"explicit-zero-limit", money(1), nil, money(0), "budget exceeded"},
		{"zero-cost", money(0), nil, money(0), ""},
		{"unknown-unconstrained", nil, nil, nil, ""},
		{"quota-exhausted", money(1), number(0), nil, "quota exhausted"},
		{"nan-quota", nil, number(math.NaN()), nil, "quota exhausted"},
		{"unknown-limit", nil, nil, &registry.Budget{Unit: "USD", Period: "task"}, ""},
		{"unit-mismatch", &registry.Budget{Amount: number(1), Unit: "tokens", Period: "task"}, nil, money(5), "incomparable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, p := fixture()
			in.Registry = in.Registry[:1]
			in.Budget = tc.limit
			in.Observations[ObservationKey{ChoiceKey{ModelKey{"codex", "codex-model"}, "low"}, "code"}] = Observation{Cost: tc.cost, Quota: tc.quota, ValidUntil: in.Now.Add(time.Minute)}
			got, err := Select(context.Background(), in, p, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.reason != "" {
				b, _ := json.Marshal(got.Excluded)
				if got.Selected != nil || !strings.Contains(string(b), tc.reason) {
					t.Fatalf("unexpected: %+v", got)
				}
			} else {
				if got.Selected == nil {
					t.Fatalf("no selection: %+v", got)
				}
				if tc.cost == nil && got.Selected.Cost != nil || tc.quota == nil && got.Selected.Quota != nil {
					t.Fatal("unknown became known")
				}
				b, err := json.Marshal(got)
				if err != nil {
					t.Fatal(err)
				}
				if tc.cost == nil && !strings.Contains(string(b), `"cost":null`) {
					t.Fatal(string(b))
				}
			}
		})
	}
	for _, scope := range []string{"provider", "model"} {
		t.Run(scope+"-budget", func(t *testing.T) {
			in, p := fixture()
			in.Registry = in.Registry[:1]
			if scope == "provider" {
				in.Registry[0].Provider.Budget = money(0)
			} else {
				in.Registry[0].Models[0].Budget = money(0)
			}
			got, err := Select(context.Background(), in, p, nil)
			if err != nil || got.Selected != nil {
				t.Fatalf("budget ignored: %+v %v", got, err)
			}
		})
	}
}
func TestJudgment(t *testing.T) {
	for _, tc := range []struct {
		name    string
		adapter Judgment
		reason  string
	}{
		{"unavailable", nil, "unavailable"},
		{"timeout", fakeAdapter(func(ctx context.Context, _ []Candidate) (JudgmentResponse, error) {
			<-ctx.Done()
			return JudgmentResponse{}, ctx.Err()
		}), "timeout"},
		{"malformed", fakeAdapter(func(context.Context, []Candidate) (JudgmentResponse, error) { return JudgmentResponse{}, nil }), "malformed"},
		{"low-confidence", fakeAdapter(func(_ context.Context, c []Candidate) (JudgmentResponse, error) {
			return JudgmentResponse{c[1].ID, 0.2, "uncertain"}, nil
		}), "low confidence"},
		{"error", fakeAdapter(func(context.Context, []Candidate) (JudgmentResponse, error) {
			return JudgmentResponse{}, errors.New("fake unavailable")
		}), "adapter error"},
		{"ineligible", fakeAdapter(func(context.Context, []Candidate) (JudgmentResponse, error) {
			return JudgmentResponse{"invented", 0.99, "choose disabled model"}, nil
		}), "ineligible"},
		{"rerank", fakeAdapter(func(_ context.Context, c []Candidate) (JudgmentResponse, error) {
			return JudgmentResponse{c[1].ID, 0.9, "fixture preference"}, nil
		}), ""},
		{"mutation", fakeAdapter(func(_ context.Context, c []Candidate) (JudgmentResponse, error) {
			c[1].Provider = "evil"
			c[1].Cost.Amount = number(999)
			return JudgmentResponse{c[1].ID, 0.9, "fixture preference"}, nil
		}), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, p := fixture()
			in.Observations[ObservationKey{ChoiceKey{ModelKey{"claude", "claude-model"}, "low"}, "code"}] = Observation{Cost: money(1), ValidUntil: in.Now.Add(time.Minute)}
			got, err := Select(context.Background(), in, p, tc.adapter)
			if err != nil {
				t.Fatal(err)
			}
			if tc.reason != "" {
				if got.Source != "rules_fallback" || got.Selected.Provider != "codex" || !strings.Contains(got.FallbackReason, tc.reason) || !strings.Contains(got.Reason, tc.reason) {
					t.Fatalf("silent/wrong fallback: %+v", got)
				}
			} else {
				if got.Source != "judgment" || got.Selected.Provider != "claude" || *got.Selected.Cost.Amount != 1 {
					t.Fatalf("invalid rerank: %+v", got)
				}
			}
		})
	}
}
func TestDecodeJudgment(t *testing.T) {
	for _, body := range []string{`{`, `{"candidate_id":"x","confidence":0.9}`, `{"candidate_id":"x","confidence":2,"reason":"x"}`, `{"candidate_id":"x","confidence":0.9,"reason":"x","extra":1}`, `{"candidate_id":"x","confidence":0.9,"reason":"x"} {}`, strings.Repeat(" ", 4097)} {
		if _, err := DecodeJudgment(strings.NewReader(body)); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
	if _, err := DecodeJudgment(strings.NewReader(`{"candidate_id":"choice-1","confidence":0.9,"reason":"fixture"}`)); err != nil {
		t.Fatal(err)
	}
}
func TestFrontierAndClosedChoices(t *testing.T) {
	in, p := fixture()
	later := in.Frontier[0]
	later.ID = "later"
	later.Issue.URL += "0"
	in.Frontier = append(in.Frontier, later)
	in.Traits["later"] = in.Traits["work"]
	adapter := fakeAdapter(func(_ context.Context, c []Candidate) (JudgmentResponse, error) {
		for _, v := range c {
			if v.WorkID != "work" {
				t.Error("judgment bypassed frontier order")
			}
		}
		return JudgmentResponse{c[0].ID, 1, "ok"}, nil
	})
	if _, err := Select(context.Background(), in, p, adapter); err != nil {
		t.Fatal(err)
	}
	in.Traits["work"] = Traits{}
	got, err := Select(context.Background(), in, p, nil)
	if err != nil || got.Selected.WorkID != "later" {
		t.Fatalf("unroutable frontier not skipped: %+v %v", got, err)
	}
	in.Frontier = nil
	called := false
	got, err = Select(context.Background(), in, p, fakeAdapter(func(context.Context, []Candidate) (JudgmentResponse, error) {
		called = true
		return JudgmentResponse{}, nil
	}))
	if err != nil || called || got.Selected != nil || got.Reason == "" {
		t.Fatalf("empty frontier: %+v %v", got, err)
	}
}

func TestEvidenceGates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Input, *Policy)
	}{
		{"disabled-provider", func(in *Input, _ *Policy) { in.Registry[0].Provider.Enabled = false }},
		{"missing-auth", func(in *Input, _ *Policy) { in.Registry[0].Auth = "unknown" }},
		{"missing-role", func(_ *Input, p *Policy) {
			c := p.Capabilities[ModelKey{"codex", "codex-model"}]
			c.Roles = nil
			p.Capabilities[ModelKey{"codex", "codex-model"}] = c
		}},
		{"unsupported-work", func(in *Input, _ *Policy) { in.Registry[0].Models[0].WorkTypes = []string{"docs"} }},
		{"stale-model", func(in *Input, _ *Policy) { in.Registry[0].Models[0].Evidence.DiscoveredAt = in.Now.Add(-time.Hour) }},
		{"future-evidence", func(in *Input, _ *Policy) { in.Registry[0].Evidence.DiscoveredAt = in.Now.Add(time.Hour) }},
		{"stale-cost", func(in *Input, _ *Policy) {
			in.Budget = money(2)
			in.Observations[ObservationKey{ChoiceKey{ModelKey{"codex", "codex-model"}, "low"}, "code"}] = Observation{Cost: money(1), ValidUntil: in.Now}
		}},
		{"missing-retry-provenance", func(in *Input, _ *Policy) { in.Frontier[0].Attempts = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, p := fixture()
			in.Registry = in.Registry[:1]
			tc.change(&in, &p)
			got, err := Select(context.Background(), in, p, nil)
			if err != nil || got.Selected != nil || len(got.Excluded) == 0 {
				t.Fatalf("gate failed: %+v %v", got, err)
			}
		})
	}
}

func TestBoundedWaitAndWireFallback(t *testing.T) {
	in, p := fixture()
	release := make(chan struct{})
	defer close(release)
	start := time.Now()
	got, err := Select(context.Background(), in, p, fakeAdapter(func(context.Context, []Candidate) (JudgmentResponse, error) {
		<-release
		return JudgmentResponse{}, nil
	}))
	if err != nil || got.Selected.Provider != "codex" || !strings.Contains(got.FallbackReason, "timeout") || time.Since(start) > time.Second {
		t.Fatalf("unbounded wait: %+v %v", got, err)
	}
	got, err = Select(context.Background(), in, p, fakeAdapter(func(context.Context, []Candidate) (JudgmentResponse, error) {
		return DecodeJudgment(strings.NewReader(`{"candidate_id":123}`))
	}))
	if err != nil || got.Selected.Provider != "codex" || !strings.Contains(got.FallbackReason, "adapter error") {
		t.Fatalf("wire error lost: %+v %v", got, err)
	}
}
