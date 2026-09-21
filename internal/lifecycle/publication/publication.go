// Package publication implements coordinator-owned publication and independent
// review assignment. It never authorizes merging or interprets CI as passed.
package publication

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PsyChaos/ai-team-harness/internal/domain"
	"github.com/PsyChaos/ai-team-harness/internal/snapshot"
)

type Identity struct {
	Repo    string `json:"repo"`
	Issue   int    `json:"issue"`
	Attempt int    `json:"attempt"`
	Head    string `json:"head_sha"`
}
type Session struct {
	Provider string `json:"provider"`
	ID       string `json:"session"`
}

// Candidate is trusted coordinator evidence of a reserved, independent session.
// Availability must reflect capability, authentication, capacity and provider health.
type Candidate struct {
	Session
	Available  bool      `json:"available"`
	ValidUntil time.Time `json:"valid_until"`
}
type Request struct {
	Identity
	Branch      string
	Graph       []byte // signed bootstrap envelope, fetched by the coordinator
	Result      []byte
	Implementer Session
	Candidates  []Candidate // preference order; different provider takes precedence
	Now         time.Time
}
type Evidence struct {
	Version int `json:"version"`
	Identity
	Branch               string      `json:"branch"`
	PR                   int         `json:"pr"`
	Role                 string      `json:"role"`
	Implementer          Session     `json:"implementer"`
	Reviewer             Session     `json:"reviewer"`
	ImplementationDigest string      `json:"implementation_result_digest"`
	GraphDigest          string      `json:"graph_digest"`
	Source               string      `json:"decision_source"`
	Reason               string      `json:"reason"`
	Candidates           []Candidate `json:"candidates"`
}
type PullRequest struct {
	Number int
	Head   string
}

// Host is the trusted coordinator I/O boundary. Implementations must serialize
// each issue/attempt, re-read authoritative issue/clone/PR state, and reject stale
// identities. Publish must verify clean isolated clone, branch, base ancestry,
// scope and exact HEAD; push without force; create/reuse the exact open PR.
// PersistRouting must durably upsert evidence in GitHub by repo/issue/attempt/head/
// role BEFORE dispatch. AssignReview must idempotently launch the recorded session
// on that immutable head, preserving metadata and pending validation evidence.
// A failed method must be recoverable by retrying the same identity.
type Host interface {
	Publish(context.Context, Request, Result) (PullRequest, error)
	PersistRouting(context.Context, Evidence) error
	AssignReview(context.Context, Evidence, Result) error
}
type Service struct {
	Signer *snapshot.Signer
	Host   Host
}

// Publish verifies the signed graph and structured result before any host call.
// The coordinator remains responsible for signed action authorization and fresh
// native-edge/publisher checks; a graph signature alone is not action authority.
func (s Service) Publish(ctx context.Context, req Request) (Evidence, error) {
	var evidence Evidence
	if err := ctx.Err(); err != nil {
		return evidence, err
	}
	if s.Host == nil || req.Repo == "" || req.Issue < 1 || req.Attempt < 1 || !sha.MatchString(req.Head) || req.Branch == "" || req.Now.IsZero() || req.Implementer.ID == "" || req.Implementer.Provider == "" {
		return evidence, fmt.Errorf("invalid publication identity")
	}
	if err := s.Signer.Verify(req.Graph); err != nil {
		return evidence, fmt.Errorf("task graph: %w", err)
	}
	if err := validateGraph(req.Graph, req.Identity); err != nil {
		return evidence, err
	}
	result, err := ParseResult(req.Result)
	if err != nil {
		return evidence, err
	}
	if result.Commit != req.Head {
		return evidence, fmt.Errorf("result/HEAD mismatch")
	}
	reviewer, reason, err := selectReviewer(req)
	if err != nil {
		return evidence, err
	}
	pr, err := s.Host.Publish(ctx, req, result)
	if err != nil {
		return evidence, err
	}
	if pr.Number < 1 || pr.Head != req.Head {
		return evidence, fmt.Errorf("published PR identity mismatch")
	}
	evidence = Evidence{Version: 1, Identity: req.Identity, Branch: req.Branch, PR: pr.Number, Role: "reviewer", Implementer: req.Implementer, Reviewer: reviewer, ImplementationDigest: result.Digest, GraphDigest: digest(string(req.Graph)), Source: "rules", Reason: reason, Candidates: append([]Candidate(nil), req.Candidates...)}
	if err = s.Host.PersistRouting(ctx, evidence); err != nil {
		return evidence, err
	}
	if err = ctx.Err(); err != nil {
		return evidence, err
	}
	return evidence, s.Host.AssignReview(ctx, evidence, result)
}

func selectReviewer(req Request) (Session, string, error) {
	var fallback *Session
	for _, c := range req.Candidates {
		if !c.Available || !req.Now.Before(c.ValidUntil) || c.ID == "" || c.ID == req.Implementer.ID || c.Provider == "" {
			continue
		}
		if c.Provider != req.Implementer.Provider {
			return c.Session, "independent session and different available provider", nil
		}
		if fallback == nil {
			v := c.Session
			fallback = &v
		}
	}
	if fallback != nil {
		return *fallback, "independent session; no different provider available", nil
	}
	return Session{}, "", fmt.Errorf("no independent review session available")
}

var nodeID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
var hexDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var bootstrapID = regexp.MustCompile(`^[0-9a-f]{24}$`)

func validateGraph(raw []byte, id Identity) error {
	var g domain.BootstrapEnvelope
	if err := json.Unmarshal(raw, &g); err != nil {
		return err
	}
	if !strings.EqualFold(g.Repo, id.Repo) || !g.Complete || g.Publisher == "" || !bootstrapID.MatchString(g.BootstrapID) || len(g.Plan.Nodes) == 0 || len(g.Plan.Nodes) > 80 || len(g.Nodes) != len(g.Plan.Nodes) {
		return fmt.Errorf("incomplete or mismatched task graph")
	}
	nodes := map[string]domain.PlanNode{}
	parents := map[string]bool{}
	numbers := map[int]bool{}
	target := ""
	for _, n := range g.Plan.Nodes {
		if !nodeID.MatchString(n.ID) || nodes[n.ID].ID != "" {
			return fmt.Errorf("duplicate or empty graph node")
		}
		if !oneOf(n.Kind, "phase", "feature", "task", "subtask") || !oneOf(n.Risk, "LOW", "MEDIUM", "HIGH", "CRITICAL") || !oneOf(n.Priority, "P0", "P1", "P2", "P3") || !oneOf(n.WorkType, "Architecture", "Backend", "Frontend", "Mobile", "Database", "DevOps", "Test", "Security", "Documentation", "Refactor", "Bug") || strings.TrimSpace(n.Title) == "" || strings.TrimSpace(n.Description) == "" || len(n.DependsOn) > 50 {
			return fmt.Errorf("invalid graph node schema")
		}
		for _, values := range [][]string{n.Acceptance, n.Validation} {
			if len(values) > 80 {
				return fmt.Errorf("oversized node evidence")
			}
			for _, value := range values {
				if strings.TrimSpace(value) == "" {
					return fmt.Errorf("empty node evidence")
				}
			}
		}
		nodes[n.ID] = n
		if n.Parent != nil {
			parents[*n.Parent] = true
		}
		b, ok := g.Nodes[n.ID]
		if !ok || b.Number < 1 || numbers[b.Number] || b.URL != fmt.Sprintf("https://github.com/%s/issues/%d", g.Repo, b.Number) || !hexDigest.MatchString(b.Digest) {
			return fmt.Errorf("invalid graph binding")
		}
		numbers[b.Number] = true
		if b.Number == id.Issue {
			target = n.ID
		}
	}
	if target == "" || parents[target] {
		return fmt.Errorf("issue is not an executable graph leaf")
	}
	state := map[string]int{}
	var visit func(string) error
	visit = func(key string) error {
		n, ok := nodes[key]
		if !ok || state[key] == 1 {
			return fmt.Errorf("unknown or cyclic graph reference")
		}
		if state[key] == 2 {
			return nil
		}
		state[key] = 1
		if !parents[key] && ((n.Kind != "task" && n.Kind != "subtask") || len(n.Acceptance) == 0 || len(n.Validation) == 0) {
			return fmt.Errorf("invalid executable leaf")
		}
		if parents[key] && len(n.DependsOn) > 0 {
			return fmt.Errorf("container dependency")
		}
		seen := map[string]bool{}
		for _, dep := range n.DependsOn {
			if parents[dep] || seen[dep] {
				return fmt.Errorf("invalid dependency")
			}
			seen[dep] = true
			if err := visit(dep); err != nil {
				return err
			}
		}
		if n.Parent != nil {
			if err := visit(*n.Parent); err != nil {
				return err
			}
		}
		state[key] = 2
		return nil
	}
	for key := range nodes {
		if err := visit(key); err != nil {
			return err
		}
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
