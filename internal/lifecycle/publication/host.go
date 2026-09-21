package publication

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Runner executes argument arrays, never shell text. The coordinator supplies a
// sanitized environment and trusted working directory/CLI binaries.
type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

// CommandRunner is for the authenticated coordinator only, never a worker.
// Env must be an explicitly constructed environment; nil inherits nothing.
type CommandRunner struct {
	Dir string
	Env []string
}

func (r CommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = r.Dir
	c.Env = append([]string{}, r.Env...)
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("%s command failed: %w", name, err)
	}
	return out, nil
}

// NativeHost publishes using git and gh directly, without the Python broker.
// Check must authenticate current publisher/graph/edges, serialize the lifecycle
// action, and sanitize isolated clone metadata (including Git config/hooks).
// Launch reserves and launches the exact session in Evidence, idempotently.
// These callbacks are required because action authority and worker reservation
// belong to the coordinator, not to a publication transport.
type NativeHost struct {
	Runner                      Runner
	Repo, Branch, Base, BaseSHA string
	Check                       func(context.Context, Request) error
	Launch                      func(context.Context, Evidence, Result) error
}

var repository = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func (h NativeHost) run(ctx context.Context, name string, args ...string) (string, error) {
	b, err := h.Runner.Run(ctx, name, args...)
	return strings.TrimSpace(string(b)), err
}
func (h NativeHost) Publish(ctx context.Context, req Request, result Result) (PullRequest, error) {
	var pr PullRequest
	if h.Runner == nil || h.Check == nil || h.Launch == nil || !repository.MatchString(h.Repo) || h.Repo != req.Repo || h.Branch != req.Branch || !strings.HasPrefix(h.Branch, fmt.Sprintf("ai/issue-%d-", req.Issue)) || !sha.MatchString(h.BaseSHA) || h.Base == "" || strings.HasPrefix(h.Base, "-") {
		return pr, fmt.Errorf("invalid native host configuration")
	}
	if err := h.Check(ctx, req); err != nil {
		return pr, err
	}
	if _, err := h.run(ctx, "git", "check-ref-format", "--branch", h.Branch); err != nil {
		return pr, err
	}
	for _, check := range []struct {
		args []string
		want string
	}{
		{[]string{"status", "--porcelain", "--untracked-files=all"}, ""},
		{[]string{"branch", "--show-current"}, h.Branch},
		{[]string{"rev-parse", "HEAD"}, result.Commit},
	} {
		got, err := h.run(ctx, "git", check.args...)
		if err != nil {
			return pr, err
		}
		if got != check.want {
			return pr, fmt.Errorf("clone state changed or dirty")
		}
	}
	if _, err := h.run(ctx, "git", "merge-base", "--is-ancestor", h.BaseSHA, result.Commit); err != nil {
		return pr, err
	}
	changed, err := h.run(ctx, "git", "diff", "--no-ext-diff", "--no-textconv", "--name-only", h.BaseSHA+"..."+result.Commit)
	if err != nil {
		return pr, err
	}
	if changed == "" {
		return pr, fmt.Errorf("empty implementation diff")
	}
	// Use a trusted URL and immutable source commit, never mutable HEAD or a remote
	// whose push URL can be rewritten by worker-owned repository configuration.
	origin := "https://github.com/" + h.Repo + ".git"
	remote, err := h.run(ctx, "git", "ls-remote", "--heads", origin, "refs/heads/"+h.Branch)
	if err != nil {
		return pr, err
	}
	if remote != "" && remote != result.Commit+"\trefs/heads/"+h.Branch {
		return pr, fmt.Errorf("remote branch has a different head; requires coordinator reconciliation")
	}
	if _, err = h.run(ctx, "git", "push", origin, result.Commit+":refs/heads/"+h.Branch); err != nil {
		return pr, err
	}
	lookup := func() (PullRequest, error) {
		raw, err := h.run(ctx, "gh", "pr", "list", "--repo", h.Repo, "--head", h.Branch, "--base", h.Base, "--state", "open", "--json", "number,headRefOid,isCrossRepository")
		if err != nil {
			return PullRequest{}, err
		}
		var rows []struct {
			Number            int
			HeadRefOid        string
			IsCrossRepository bool
		}
		if err = json.Unmarshal([]byte(raw), &rows); err != nil {
			return PullRequest{}, err
		}
		if len(rows) == 0 {
			return PullRequest{}, nil
		}
		if len(rows) != 1 || rows[0].IsCrossRepository {
			return PullRequest{}, fmt.Errorf("ambiguous publication PR")
		}
		return PullRequest{rows[0].Number, rows[0].HeadRefOid}, nil
	}
	pr, err = lookup()
	if err != nil {
		return pr, err
	}
	if pr.Number == 0 {
		_, createErr := h.run(ctx, "gh", "pr", "create", "--repo", h.Repo, "--base", h.Base, "--head", h.Branch, "--title", fmt.Sprintf("Issue #%d implementation", req.Issue), "--body", fmt.Sprintf("Closes #%d\n\nImplementation: %s\nResult digest: %s\nPending external checks: %d\n", req.Issue, result.Commit, result.Digest, len(result.PendingValidation)))
		pr, err = lookup() // recover a successful remote creation after a CLI failure
		if err != nil {
			return pr, err
		}
		if pr.Number == 0 {
			return pr, fmt.Errorf("PR creation failed: %v", createErr)
		}
	}
	if pr.Head != result.Commit {
		return pr, fmt.Errorf("published head mismatch")
	}
	return pr, nil
}

func (h NativeHost) PersistRouting(ctx context.Context, e Evidence) error {
	if h.Runner == nil || h.Repo != e.Repo {
		return fmt.Errorf("routing repository mismatch")
	}
	// Paginate so an old assignment cannot disappear behind the API's first page.
	endpoint := fmt.Sprintf("repos/%s/issues/%d/comments", h.Repo, e.Issue)
	raw, err := h.run(ctx, "gh", "api", "--paginate", "--slurp", endpoint)
	if err != nil {
		return err
	}
	var pages [][]struct {
		ID   int
		Body string
	}
	if err = json.Unmarshal([]byte(raw), &pages); err != nil {
		return err
	}
	marker := fmt.Sprintf("<!-- ai-harness-review-assignment:v1:%d:%d:%s:%s -->", e.Issue, e.Attempt, e.Head, e.Role)
	document, err := json.Marshal(e)
	if err != nil {
		return err
	}
	body := marker + "\n" + string(document)
	// Immutable evidence: identical replays are harmless; conflicting assignments
	// must not overwrite the session that might already be running.
	found := false
	for _, page := range pages {
		for _, c := range page {
			if strings.HasPrefix(c.Body, marker+"\n") {
				if c.Body != body {
					return fmt.Errorf("conflicting durable review assignment")
				}
				found = true
			}
		}
	}
	if found {
		return nil
	}
	_, err = h.run(ctx, "gh", "api", "--method", "POST", endpoint, "-f", "body="+body)
	return err
}
func (h NativeHost) AssignReview(ctx context.Context, e Evidence, r Result) error {
	if h.Launch == nil || e.Repo != h.Repo || e.PR < 1 {
		return fmt.Errorf("invalid review launch")
	}
	raw, err := h.run(ctx, "gh", "pr", "view", strconv.Itoa(e.PR), "--repo", h.Repo, "--json", "headRefOid,state")
	if err != nil {
		return err
	}
	var pr struct{ HeadRefOid, State string }
	if err = json.Unmarshal([]byte(raw), &pr); err != nil {
		return err
	}
	if pr.HeadRefOid != e.Head || pr.State != "OPEN" {
		return fmt.Errorf("review PR changed")
	}
	return h.Launch(ctx, e, r)
}
