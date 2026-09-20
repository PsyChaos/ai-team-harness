package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/PsyChaos/ai-team-harness/internal/registry"
	"github.com/PsyChaos/ai-team-harness/internal/scheduler"
)

// ModelKey uses the registry label (including its explicit "CLI default" sentinel).
type ModelKey struct{ Provider, Model string }
type ChoiceKey struct {
	ModelKey
	Effort string
}

// Capability is an operator assertion, never inferred from vendor model names.
type Capability struct {
	Profiles []string
	Roles    []scheduler.Role
}
type Traits struct{ Complexity, Risk, WorkType, PreviousProvider string }

// Observation is scoped to this model/effort and work type. Nil cost/quota means
// unknown. Quota is remaining executions; cost and limits must share unit/period.
type Observation struct {
	Cost                *registry.Budget
	Quota               *float64
	Successes, Failures int
	ValidUntil          time.Time
}
type ObservationKey struct {
	ChoiceKey
	WorkType string
}
type Policy struct {
	Version             string
	HealthTTL           time.Duration
	Capabilities        map[ModelKey]Capability
	ProviderOrder       []string
	EffortOrder         map[string][]string // profile -> preferred supported efforts
	SwitchOnLastAttempt bool
	JudgmentTimeout     time.Duration
	MinConfidence       float64
}

type Input struct {
	// Frontier must come from scheduler.Frontier for the same coherent snapshot.
	Frontier     []scheduler.Work
	Now          time.Time
	Providers    map[string]scheduler.Provider
	Registry     []registry.Entry
	Traits       map[string]Traits // work ID -> classification
	Observations map[ObservationKey]Observation
	Budget       *registry.Budget // remaining task budget; nil means no declared limit
}

type Tuple struct {
	WorkID   string         `json:"work_id"`
	Issue    string         `json:"issue"`
	Role     scheduler.Role `json:"role"`
	Provider string         `json:"provider"`
	Model    string         `json:"model"`
	Effort   string         `json:"effort"`
	Profile  string         `json:"capability_profile"`
}
type Candidate struct {
	ID string `json:"id"`
	Tuple
	Cost      *registry.Budget `json:"cost"`
	Quota     *float64         `json:"quota"`
	Successes int              `json:"successes"`
	Failures  int              `json:"failures"`
}
type Exclusion struct{ WorkID, Provider, Model, Effort, Reason string }
type Result struct {
	Selected       *Candidate  `json:"selected"`
	Source         string      `json:"decision_source"`
	Reason         string      `json:"reason"`
	PolicyVersion  string      `json:"policy_version"`
	FallbackReason string      `json:"fallback_reason,omitempty"`
	Excluded       []Exclusion `json:"excluded"`
}

// Judgment may only select an ID from the supplied candidates. Implementations
// must honor ctx cancellation and bound subprocess/output resources themselves.
// An adapter can translate this contract to Jev without granting execution rights.
type Judgment interface {
	Rank(context.Context, []Candidate) (JudgmentResponse, error)
}
type JudgmentResponse struct {
	CandidateID string  `json:"candidate_id"`
	Confidence  float64 `json:"confidence"`
	Reason      string  `json:"reason"`
}

// DecodeJudgment is a strict bounded wire decoder for CLI adapters.
func DecodeJudgment(r io.Reader) (JudgmentResponse, error) {
	var response JudgmentResponse
	data, err := io.ReadAll(io.LimitReader(r, 4097))
	if err != nil {
		return response, err
	}
	if len(data) > 4096 {
		return response, fmt.Errorf("judgment exceeds 4096 bytes")
	}
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	if err := d.Decode(&response); err != nil {
		return response, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return response, fmt.Errorf("expected one judgment document")
	}
	if !validResponse(response) {
		return response, fmt.Errorf("malformed judgment")
	}
	return response, nil
}

func validResponse(r JudgmentResponse) bool {
	return r.CandidateID != "" && strings.TrimSpace(r.Reason) != "" && len(r.Reason) <= 2048 && finite(r.Confidence) && r.Confidence >= 0 && r.Confidence <= 1
}

// Select returns evidence, not a reservation. Re-observe and reserve before dispatch.
// The first routable frontier work retains scheduler priority; judgment only ranks
// valid tuples for that work, so it cannot defeat aging or dependency governance.
func Select(ctx context.Context, in Input, p Policy, adapter Judgment) (Result, error) {
	result := Result{Source: "rules", PolicyVersion: p.Version}
	if p.Version == "" || p.HealthTTL <= 0 || in.Now.IsZero() || p.JudgmentTimeout <= 0 || p.JudgmentTimeout > 30*time.Second || !finite(p.MinConfidence) || p.MinConfidence <= 0 || p.MinConfidence > 1 {
		return result, fmt.Errorf("invalid routing policy or observation time")
	}
	// Reject ambiguous registry identities before generating candidates.
	config := registry.Config{}
	for _, e := range in.Registry {
		provider := e.Provider
		provider.Models = nil
		for _, m := range e.Models {
			provider.Models = append(provider.Models, m.Model)
		}
		config.Providers = append(config.Providers, provider)
	}
	if err := config.Validate(); err != nil {
		return result, err
	}
	var candidates []Candidate
	for _, w := range in.Frontier {
		t, ok := in.Traits[w.ID]
		profile := classify(t)
		if !ok || profile == "" {
			result.Excluded = append(result.Excluded, Exclusion{WorkID: w.ID, Reason: "missing or invalid task classification"})
			continue
		}
		for _, e := range in.Registry {
			for _, m := range e.Models {
				for _, effort := range unique(m.Efforts) {
					key := ChoiceKey{ModelKey{e.Provider.Name, m.Model.Label()}, effort}
					observation := in.Observations[ObservationKey{key, t.WorkType}]
					if !in.Now.Before(observation.ValidUntil) {
						observation = Observation{}
					}
					c := Candidate{Tuple: Tuple{w.ID, w.Issue.URL, w.Role, key.Provider, key.Model, effort, profile}, Cost: observation.Cost, Quota: observation.Quota, Successes: observation.Successes, Failures: observation.Failures}
					reason := excluded(in, p, w, t, e, m, c)
					if reason != "" {
						result.Excluded = append(result.Excluded, Exclusion{w.ID, key.Provider, key.Model, effort, reason})
						continue
					}
					candidates = append(candidates, c)
				}
			}
		}
		if len(candidates) > 0 {
			break
		}
	}
	sort.Slice(result.Excluded, func(i, j int) bool {
		a, _ := json.Marshal(result.Excluded[i])
		b, _ := json.Marshal(result.Excluded[j])
		return string(a) < string(b)
	})
	if len(candidates) == 0 {
		result.Reason = "no eligible tuple satisfies capability, availability and budget constraints"
		return result, nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		// Observed net successes are evidence, not a claim about absolute quality.
		scoreA, scoreB := float64(a.Successes)-float64(a.Failures), float64(b.Successes)-float64(b.Failures)
		if scoreA != scoreB {
			return scoreA > scoreB
		}
		if x, y := index(p.ProviderOrder, a.Provider), index(p.ProviderOrder, b.Provider); x != y {
			return x < y
		}
		if x, y := index(p.EffortOrder[a.Profile], a.Effort), index(p.EffortOrder[b.Profile], b.Effort); x != y {
			return x < y
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Effort < b.Effort
	})
	for i := range candidates {
		candidates[i].ID = fmt.Sprintf("choice-%d", i+1)
	}
	result.Selected = &candidates[0]
	result.Reason = "first routable frontier work; profile=" + candidates[0].Profile + "; ranked by observed net successes, provider preference, effort preference, lexical identity; unknown cost/quota remain null"
	fallback := func(reason string) (Result, error) {
		result.Source = "rules_fallback"
		result.FallbackReason = reason
		result.Reason += "; judgment fallback: " + reason
		return result, nil
	}
	if adapter == nil {
		return fallback("adapter unavailable")
	}
	deadline, cancel := context.WithTimeout(ctx, p.JudgmentTimeout)
	defer cancel()
	// Deep-copy evidence so an adapter cannot mutate the validated choices.
	data, _ := json.Marshal(candidates)
	var offered []Candidate
	if err := json.Unmarshal(data, &offered); err != nil {
		return result, err
	}
	type answer struct {
		response JudgmentResponse
		err      error
	}
	ch := make(chan answer, 1)
	go func() { r, err := adapter.Rank(deadline, offered); ch <- answer{r, err} }()
	var a answer
	select {
	case <-deadline.Done():
		return fallback("timeout or cancellation: " + deadline.Err().Error())
	case a = <-ch:
	}
	if deadline.Err() != nil {
		return fallback("timeout or cancellation: " + deadline.Err().Error())
	}
	if a.err != nil {
		return fallback("adapter error: " + bounded(a.err.Error()))
	}
	if !validResponse(a.response) {
		return fallback("malformed response")
	}
	if a.response.Confidence < p.MinConfidence {
		return fallback("low confidence")
	}
	for i := range candidates {
		if candidates[i].ID == a.response.CandidateID {
			result.Selected = &candidates[i]
			result.Source = "judgment"
			result.Reason = "eligible candidate re-ranked: " + a.response.Reason
			return result, nil
		}
	}
	return fallback("response selected an ineligible candidate")
}

func excluded(in Input, p Policy, w scheduler.Work, t Traits, e registry.Entry, m registry.ModelEntry, c Candidate) string {
	health := in.Providers[c.Provider]
	if !contains(w.Providers, c.Provider) || !e.Provider.Enabled || !e.Available || !e.Installed || e.Auth != "authenticated" || e.Provider.Concurrency <= 0 {
		return "provider unavailable or incompatible"
	}
	if !health.Healthy || health.Available <= 0 || !in.Now.Before(health.ValidUntil) {
		return "provider outage, stale health or exhausted capacity"
	}
	if !fresh(in.Now, e.Evidence.DiscoveredAt, p.HealthTTL) || !fresh(in.Now, m.Evidence.DiscoveredAt, p.HealthTTL) {
		return "stale registry evidence"
	}
	if !m.Enabled || !m.Available {
		return "model disabled or unavailable"
	}
	capability := p.Capabilities[ModelKey{c.Provider, c.Model}]
	roleOK := false
	for _, role := range capability.Roles {
		if role == w.Role {
			roleOK = true
		}
	}
	if !roleOK || !contains(capability.Profiles, c.Profile) || !contains(m.WorkTypes, t.WorkType) {
		return "unsupported role, profile or work type"
	}
	if c.Effort == "" || !contains(p.EffortOrder[c.Profile], c.Effort) {
		return "unsupported effort"
	}
	if p.SwitchOnLastAttempt && w.Attempts > 0 && w.Attempts == w.MaxAttempts-1 && c.Provider == t.PreviousProvider {
		return "last retry requires provider switch"
	}
	if p.SwitchOnLastAttempt && w.Attempts > 0 && w.Attempts == w.MaxAttempts-1 && t.PreviousProvider == "" {
		return "last retry missing previous provider"
	}
	if c.Successes < 0 || c.Failures < 0 {
		return "invalid observed results"
	}
	if c.Quota != nil && (!finite(*c.Quota) || *c.Quota < 1) {
		return "quota exhausted or invalid"
	}
	if c.Cost != nil && (c.Cost.Unit == "" || c.Cost.Period == "" || c.Cost.Amount != nil && (!finite(*c.Cost.Amount) || *c.Cost.Amount < 0)) {
		return "invalid cost evidence"
	}
	for _, limit := range []*registry.Budget{in.Budget, e.Provider.Budget, m.Budget} {
		if limit == nil {
			continue
		}
		if limit.Unit == "" || limit.Period == "" || limit.Amount != nil && (!finite(*limit.Amount) || *limit.Amount < 0) {
			return "invalid budget"
		}
		if limit.Amount == nil {
			continue
		}
		if c.Cost == nil || c.Cost.Amount == nil {
			return "unknown cost cannot satisfy hard budget"
		}
		if limit.Unit != c.Cost.Unit || limit.Period != c.Cost.Period {
			return "incomparable budget units or period"
		}
		if *c.Cost.Amount > *limit.Amount {
			return "budget exceeded"
		}
	}
	return ""
}
func classify(t Traits) string {
	switch t.Risk {
	case "LOW", "MEDIUM", "HIGH", "CRITICAL":
	default:
		return ""
	}
	switch t.Complexity {
	case "small", "normal", "complex":
	default:
		return ""
	}
	if t.WorkType == "" {
		return ""
	}
	if t.Risk == "HIGH" || t.Risk == "CRITICAL" || t.Complexity == "complex" || contains([]string{"security", "architecture", "concurrency", "state", "data_integrity", "debugging"}, t.WorkType) {
		return "strong"
	}
	if t.Risk == "LOW" && (t.Complexity == "small" || t.WorkType == "docs") {
		return "fast"
	}
	return "balanced"
}
func contains(values []string, v string) bool { return index(values, v) < len(values) }
func index(values []string, v string) int {
	for i, s := range values {
		if s == v {
			return i
		}
	}
	return len(values)
}
func unique(values []string) []string {
	var out []string
	for _, v := range values {
		if !contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func fresh(now, at time.Time, ttl time.Duration) bool {
	return !at.IsZero() && !at.After(now) && now.Sub(at) < ttl
}
func bounded(s string) string {
	if len(s) > 2048 {
		return s[:2048]
	}
	return s
}
