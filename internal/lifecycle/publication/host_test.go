package publication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type commandStep struct {
	name   string
	args   []string
	output string
	err    error
}
type scriptRunner struct {
	t     *testing.T
	steps []commandStep
}

func (r *scriptRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.t.Helper()
	if len(r.steps) == 0 {
		r.t.Fatalf("unexpected call %s %v", name, args)
	}
	step := r.steps[0]
	r.steps = r.steps[1:]
	if name != step.name || !reflect.DeepEqual(args, step.args) {
		r.t.Fatalf("got %s %v; want %s %v", name, args, step.name, step.args)
	}
	return []byte(step.output), step.err
}
func TestNativePublicationAndDurableAssignment(t *testing.T) {
	s, req, _ := fixture(t)
	result, err := ParseResult(req.Result)
	if err != nil {
		t.Fatal(err)
	}
	base := strings.Repeat("b", 40)
	prJSON := `[{"number":42,"headRefOid":"` + req.Head + `","isCrossRepository":false}]`
	runner := &scriptRunner{t: t, steps: []commandStep{
		{"git", []string{"check-ref-format", "--branch", req.Branch}, "", nil},
		{"git", []string{"status", "--porcelain", "--untracked-files=all"}, "", nil},
		{"git", []string{"branch", "--show-current"}, req.Branch, nil},
		{"git", []string{"rev-parse", "HEAD"}, req.Head, nil},
		{"git", []string{"merge-base", "--is-ancestor", base, req.Head}, "", nil},
		{"git", []string{"diff", "--no-ext-diff", "--no-textconv", "--name-only", base + "..." + req.Head}, "publication.go", nil},
		{"git", []string{"ls-remote", "--heads", "https://github.com/test/repo.git", "refs/heads/" + req.Branch}, "", nil},
		{"git", []string{"push", "https://github.com/test/repo.git", req.Head + ":refs/heads/" + req.Branch}, "", nil},
		{"gh", []string{"pr", "list", "--repo", req.Repo, "--head", req.Branch, "--base", "main", "--state", "open", "--json", "number,headRefOid,isCrossRepository"}, prJSON, nil},
	}}
	checked, launched := false, false
	host := NativeHost{Runner: runner, Repo: req.Repo, Branch: req.Branch, Base: "main", BaseSHA: base, Check: func(context.Context, Request) error { checked = true; return nil }, Launch: func(_ context.Context, e Evidence, r Result) error {
		launched = true
		if e.Head != req.Head || !reflect.DeepEqual(r, result) {
			t.Fatal("launch evidence mismatch")
		}
		return nil
	}}
	// Exercise the production host through Service with exact durable JSON.
	reviewer, reason, err := selectReviewer(req)
	if err != nil {
		t.Fatal(err)
	}
	e := Evidence{Version: 1, Identity: req.Identity, Branch: req.Branch, PR: 42, Role: "reviewer", Implementer: req.Implementer, Reviewer: reviewer, ImplementationDigest: result.Digest, GraphDigest: digest(string(req.Graph)), Source: "rules", Reason: reason, Candidates: req.Candidates}
	// Simulate a create that succeeded remotely but whose CLI response failed.
	lookup := runner.steps[len(runner.steps)-1]
	runner.steps[len(runner.steps)-1].output = "[]"
	runner.steps = append(runner.steps, commandStep{"gh", []string{"pr", "create", "--repo", req.Repo, "--base", "main", "--head", req.Branch, "--title", "Issue #3 implementation", "--body", fmt.Sprintf("Closes #3\n\nImplementation: %s\nResult digest: %s\nPending external checks: 1\n", req.Head, result.Digest)}, "", errors.New("response lost")}, lookup)
	doc, _ := json.Marshal(e)
	body := "<!-- ai-harness-review-assignment:v1:3:2:" + req.Head + ":reviewer -->\n" + string(doc)
	runner.steps = append(runner.steps,
		commandStep{"gh", []string{"api", "--paginate", "--slurp", "repos/test/repo/issues/3/comments"}, "[[]]", nil},
		commandStep{"gh", []string{"api", "--method", "POST", "repos/test/repo/issues/3/comments", "-f", "body=" + body}, "{}", nil},
		commandStep{"gh", []string{"pr", "view", "42", "--repo", req.Repo, "--json", "headRefOid,state"}, `{"headRefOid":"` + req.Head + `","state":"OPEN"}`, nil},
	)
	s.Host = host
	if _, err = s.Publish(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !checked || !launched || len(runner.steps) != 0 {
		t.Fatal("missing host operations")
	}
	// Replay finds an identical durable assignment on a later page, without POST.
	comment, _ := json.Marshal([][]map[string]any{{}, {{"id": 10, "body": body}}})
	runner.steps = []commandStep{{"gh", []string{"api", "--paginate", "--slurp", "repos/test/repo/issues/3/comments"}, string(comment), nil}}
	if err = host.PersistRouting(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	e.Reviewer.ID = "replacement-session"
	runner.steps = []commandStep{{"gh", []string{"api", "--paginate", "--slurp", "repos/test/repo/issues/3/comments"}, string(comment), nil}}
	if err = host.PersistRouting(context.Background(), e); err == nil {
		t.Fatal("overwrote existing session")
	}
}
func TestNativeHostAuthorizationFailure(t *testing.T) {
	_, req, _ := fixture(t)
	result, _ := ParseResult(req.Result)
	h := NativeHost{Runner: &scriptRunner{t: t}, Repo: req.Repo, Branch: req.Branch, Base: "main", BaseSHA: strings.Repeat("b", 40), Check: func(context.Context, Request) error { return errors.New("stale action") }, Launch: func(context.Context, Evidence, Result) error { return nil }}
	if _, err := h.Publish(context.Background(), req, result); err == nil {
		t.Fatal("accepted stale authorization")
	}
}
