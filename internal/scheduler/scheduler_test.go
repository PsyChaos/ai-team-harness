package scheduler

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/PsyChaos/ai-team-harness/internal/domain"
)

func work(id string) Work {
	return Work{ID: id, Issue: domain.Issue{URL: "https://example.test/repo/issues/" + id, State: "OPEN", Repository: domain.Repository{NameWithOwner: "owner/repo"}, BlockedBy: domain.Blockers{Complete: true}}, Status: "READY", Role: Implementation, MaxAttempts: 2, Providers: []string{"healthy"}}
}

func snapshot(items ...Work) Snapshot {
	now := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	return Snapshot{Now: now, Work: items, Available: 1, RoleAvailable: map[Role]int{Implementation: 1, Review: 1, Recovery: 1}, Providers: map[string]Provider{"healthy": {Healthy: true, Available: 1, ValidUntil: now.Add(time.Minute)}}}
}

func ids(items []Work) []string {
	result := make([]string, 0, len(items))
	for _, w := range items {
		result = append(result, w.ID)
	}
	return result
}

func TestEligibilityGraph(t *testing.T) {
	ready := work("ready")
	satisfied := work("satisfied")
	satisfied.Issue.BlockedBy.Nodes = []domain.Dependency{{URL: "dependency", State: "CLOSED"}}
	blocked := work("blocked")
	blocked.Issue.BlockedBy.Nodes = []domain.Dependency{{URL: ready.Issue.URL, State: "OPEN"}}
	unknown := work("unknown")
	unknown.Issue.BlockedBy.Complete = false
	leased := work("leased")
	exhausted := work("exhausted")
	exhausted.Attempts = 2
	file := work("file")
	file.Files = []string{"internal/scheduler/code.go"}
	resource := work("resource")
	resource.Resources = []string{"database"}
	closed := work("closed")
	closed.Issue.State = "CLOSED"
	s := snapshot(ready, satisfied, blocked, unknown, leased, exhausted, file, resource, closed)
	s.Leases = []Lease{{IssueURL: leased.Issue.URL}, {Repository: "owner/repo", Files: []string{"internal/scheduler"}}, {Resources: []string{"database"}}}
	scheduler := New(2)
	for round := 0; round < 6; round++ {
		if got := ids(scheduler.Frontier(s)); !reflect.DeepEqual(got, []string{"ready", "satisfied"}) {
			t.Fatalf("round %d: %v", round, got)
		}
	}
}

func TestHardGates(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Snapshot)
	}{
		{"global capacity", func(s *Snapshot) { s.Available = 0 }},
		{"role capacity", func(s *Snapshot) { delete(s.RoleAvailable, Implementation) }},
		{"unknown provider", func(s *Snapshot) { s.Work[0].Providers = []string{"missing"} }},
		{"unhealthy provider", func(s *Snapshot) { p := s.Providers["healthy"]; p.Healthy = false; s.Providers["healthy"] = p }},
		{"stale provider", func(s *Snapshot) { p := s.Providers["healthy"]; p.ValidUntil = s.Now; s.Providers["healthy"] = p }},
		{"full provider", func(s *Snapshot) { p := s.Providers["healthy"]; p.Available = 0; s.Providers["healthy"] = p }},
		{"missing time", func(s *Snapshot) { s.Now = time.Time{} }},
		{"wrong issue state", func(s *Snapshot) { s.Work[0].Status = "BACKLOG" }},
		{"unknown role", func(s *Snapshot) { s.Work[0].Role = "unknown" }},
		{"unknown dependency", func(s *Snapshot) { s.Work[0].Issue.BlockedBy.Nodes = []domain.Dependency{{State: ""}} }},
		{"negative attempts", func(s *Snapshot) { s.Work[0].Attempts = -1 }},
		{"no attempts allowed", func(s *Snapshot) { s.Work[0].MaxAttempts = 0 }},
		{"invalid file", func(s *Snapshot) { s.Work[0].Files = []string{"../escape"} }},
		{"duplicate ID", func(s *Snapshot) { s.Work = append(s.Work, s.Work[0]) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := work("candidate")
			w.Priority = 999
			s := snapshot(w)
			scheduler := New(1)
			scheduler.Frontier(s)
			scheduler.Frontier(s) // Already promoted before losing eligibility.
			tc.change(&s)
			if got := scheduler.Frontier(s); len(got) != 0 {
				t.Fatalf("hard gate overridden: %v", ids(got))
			}
		})
	}
}

func TestFileAndResourceReservations(t *testing.T) {
	cases := []struct {
		name, repo, file, claim string
		conflict                bool
	}{
		{"same file", "owner/repo", "src/a.go", "src/a.go", true},
		{"directory", "owner/repo", "src/a.go", "src", true},
		{"parent claim", "owner/repo", "src", "src/a.go", true},
		{"path normalization", "owner/repo", "src/../src/a.go", "src", true},
		{"root", "owner/repo", "src/a.go", ".", true},
		{"prefix sibling", "owner/repo", "src-other/a.go", "src", false},
		{"different repo", "other/repo", "src/a.go", "src", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := work("file")
			w.Files = []string{tc.file}
			s := snapshot(w)
			s.Leases = []Lease{{Repository: tc.repo, Files: []string{tc.claim}}}
			if got := len(New(2).Frontier(s)) == 0; got != tc.conflict {
				t.Fatalf("conflict=%v", got)
			}
		})
	}
	w := work("available")
	w.Providers = []string{"missing", "healthy"}
	if len(New(2).Frontier(snapshot(w))) != 1 {
		t.Fatal("healthy alternative must suffice")
	}
}

func TestReviewAndRecoveryPriority(t *testing.T) {
	implementation := work("implementation")
	implementation.Priority = 100
	review := work("review")
	review.Role = Review
	review.Status = "IMPLEMENTED"
	recovery := work("recovery")
	recovery.Role = Recovery
	recovery.Status = "REVIEWING"
	s := snapshot(implementation, review, recovery)
	want := []string{"recovery", "review", "implementation"}
	if got := ids(New(3).Frontier(s)); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	s.Work = []Work{recovery, implementation, review}
	if got := ids(New(3).Frontier(s)); !reflect.DeepEqual(got, want) {
		t.Fatalf("input order changed ranking: %v", got)
	}
}

func TestBoundedAgingUnderContinuousReviews(t *testing.T) {
	const limit = 3
	scheduler := New(limit)
	target := work("target")
	target.Priority = -100
	older := work("older")
	older.Priority = -50
	// One item is already waiting when target arrives.
	scheduler.Frontier(snapshot(older))
	pending := []Work{older, target}
	const bound = limit + 1 + 1
	for round := 1; round <= bound; round++ {
		review := work(fmt.Sprintf("review-%d", round))
		review.Role = Review
		review.Status = "IMPLEMENTED"
		pending = append(pending, review)
		got := scheduler.Frontier(snapshot(pending...))
		if round <= limit && got[0].ID == target.ID {
			t.Fatalf("target promoted prematurely in round %d", round)
		}
		if got[0].ID == target.ID {
			return
		}
		scheduler.Dispatched(got[0].ID)
		for i, w := range pending {
			if w.ID == got[0].ID {
				pending = append(pending[:i], pending[i+1:]...)
				break
			}
		}
	}
	t.Fatalf("target not dispatched within documented %d-round bound", bound)
}

func TestAgingResetsAndNeverBypassesEligibility(t *testing.T) {
	scheduler := New(1)
	w := work("work")
	s := snapshot(w)
	scheduler.Frontier(s)
	scheduler.Frontier(s)
	scheduler.Dispatched(w.ID)
	review := work("review")
	review.Role = Review
	review.Status = "IMPLEMENTED"
	s.Work = append(s.Work, review)
	if got := scheduler.Frontier(s); got[0].ID != "review" {
		t.Fatal("dispatch did not reset aging")
	}
	s.Work[0].Issue.BlockedBy.Complete = false
	if got := ids(scheduler.Frontier(s)); !reflect.DeepEqual(got, []string{"review"}) {
		t.Fatalf("aged blocked work eligible: %v", got)
	}
	s.Work[0].Issue.BlockedBy.Complete = true
	if got := scheduler.Frontier(s); got[0].ID != "review" {
		t.Fatal("ineligible work retained aging")
	}
}

func TestFrontierRechecksReservationsBetweenDispatches(t *testing.T) {
	a, b := work("a"), work("b")
	a.Resources = []string{"exclusive"}
	b.Resources = []string{"exclusive"}
	s := snapshot(a, b)
	scheduler := New(2)
	if got := ids(scheduler.Frontier(s)); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("individual frontier: %v", got)
	}
	scheduler.Dispatched(a.ID)
	s.Leases = []Lease{{IssueURL: a.Issue.URL, Resources: a.Resources}}
	if got := scheduler.Frontier(s); len(got) != 0 {
		t.Fatalf("reservation ignored: %v", ids(got))
	}
}
