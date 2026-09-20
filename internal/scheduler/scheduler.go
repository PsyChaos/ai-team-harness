package scheduler

import (
	"path"
	"sort"
	"strings"
	"time"

	"github.com/PsyChaos/ai-team-harness/internal/domain"
)

type Role string

const (
	Implementation Role = "implementation"
	Review         Role = "review"
	Recovery       Role = "recovery"
)

// Work is one pending action. ID must be unique and stable across rounds;
// Issue.URL identifies the issue across repositories. Attempts counts starts.
type Work struct {
	ID          string
	Issue       domain.Issue
	Status      string
	Role        Role
	Attempts    int
	MaxAttempts int
	Priority    int      // Larger values rank first, below aging and review/recovery.
	Files       []string // Repository-relative paths; directories claim their descendants.
	Resources   []string // Opaque exclusive resource names, shared across repositories.
	Providers   []string // Compatible providers, determined by the caller.
}

// Lease contains only currently active reservations, as verified by the caller.
// Files are scoped to Repository; resources and issue identities are global.
type Lease struct {
	IssueURL   string
	Repository string
	Files      []string
	Resources  []string
}

type Provider struct {
	Healthy    bool
	ValidUntil time.Time
	Available  int
}

// Snapshot is an exact, coherent observation. Available capacity already
// subtracts active reservations. Missing health/capacity is unavailable.
type Snapshot struct {
	Now           time.Time
	Work          []Work
	Leases        []Lease
	Available     int
	RoleAvailable map[Role]int
	Providers     map[string]Provider
}

type waiting struct {
	id  string
	age int
}

// Scheduler retains disposable aging state, not execution authority. It must
// be used serially. A round is one Frontier call, not elapsed wall time.
type Scheduler struct {
	threshold int
	waiting   []waiting
}

// New promotes work after ageLimit consecutive eligible rounds without dispatch.
func New(ageLimit int) *Scheduler {
	if ageLimit < 1 {
		ageLimit = 1
	}
	return &Scheduler{threshold: ageLimit}
}

// Dispatched resets aging after the caller successfully reserves/dispatches work.
// Failed dispatches should retain their position. Missing/ineligible work is
// automatically forgotten on the next Frontier call.
func (s *Scheduler) Dispatched(id string) {
	for i, w := range s.waiting {
		if w.id == id {
			s.waiting = append(s.waiting[:i], s.waiting[i+1:]...)
			return
		}
	}
}

// Frontier returns all individually eligible actions in deterministic rank order.
// It is NOT a conflict-free dispatch batch: reserve one action, update the exact
// snapshot, and re-evaluate before dispatching another. No provider is selected.
func (s *Scheduler) Frontier(snapshot Snapshot) []Work {
	counts := make(map[string]int)
	for _, w := range snapshot.Work {
		counts[w.ID]++
	}
	eligible := make(map[string]Work)
	for _, w := range snapshot.Work {
		if counts[w.ID] == 1 && allowed(w, snapshot) {
			eligible[w.ID] = w
		}
	}
	// Preserve FIFO arrival order independent of input ordering. Ages saturate.
	next := make([]waiting, 0, len(eligible))
	seen := make(map[string]bool)
	for _, previous := range s.waiting {
		if _, ok := eligible[previous.id]; !ok {
			continue
		}
		if previous.age < s.threshold {
			previous.age++
		}
		next = append(next, previous)
		seen[previous.id] = true
	}
	var added []string
	for id := range eligible {
		if !seen[id] {
			added = append(added, id)
		}
	}
	sort.Strings(added)
	for _, id := range added {
		next = append(next, waiting{id: id})
	}
	s.waiting = next
	order := make(map[string]int)
	ages := make(map[string]int)
	result := make([]Work, 0, len(next))
	for i, w := range next {
		order[w.id] = i
		ages[w.id] = w.age
		result = append(result, eligible[w.id])
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		agedA, agedB := ages[a.ID] >= s.threshold, ages[b.ID] >= s.threshold
		if agedA != agedB {
			return agedA
		}
		if agedA {
			return order[a.ID] < order[b.ID]
		}
		finishA, finishB := a.Role != Implementation, b.Role != Implementation
		if finishA != finishB {
			return finishA
		}
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		return a.ID < b.ID
	})
	return result
}

func allowed(w Work, s Snapshot) bool {
	if w.ID == "" || w.Issue.URL == "" || w.Issue.Repository.NameWithOwner == "" || w.Issue.State != "OPEN" ||
		!w.Issue.BlockedBy.Complete || w.Attempts < 0 || w.MaxAttempts <= w.Attempts ||
		s.Now.IsZero() || s.Available <= 0 || s.RoleAvailable[w.Role] <= 0 {
		return false
	}
	switch w.Role {
	case Implementation:
		if w.Status != "READY" && w.Status != "CHANGES_REQUESTED" && w.Status != "RETRY_PENDING" {
			return false
		}
	case Review:
		if w.Status != "IMPLEMENTED" {
			return false
		}
	case Recovery:
		if w.Status != "CLAIMED" && w.Status != "IN_PROGRESS" && w.Status != "REVIEWING" {
			return false
		}
	default:
		return false
	}
	for _, d := range w.Issue.BlockedBy.Nodes {
		if d.State != "CLOSED" {
			return false
		}
	}
	for _, file := range w.Files {
		if !validFile(file) {
			return false
		}
	}
	for _, resource := range w.Resources {
		if resource == "" {
			return false
		}
	}
	for _, lease := range s.Leases {
		if lease.IssueURL == w.Issue.URL || overlaps(w.Resources, lease.Resources) {
			return false
		}
		if lease.Repository == w.Issue.Repository.NameWithOwner {
			for _, a := range w.Files {
				for _, b := range lease.Files {
					// An invalid active claim cannot safely be treated as nonconflicting.
					if !validFile(b) || fileOverlap(a, b) {
						return false
					}
				}
			}
		}
	}
	for _, name := range w.Providers {
		p, ok := s.Providers[name]
		if ok && p.Healthy && p.Available > 0 && s.Now.Before(p.ValidUntil) {
			return true
		}
	}
	return false
}

func validFile(p string) bool {
	return p != "" && !strings.Contains(p, "\\") && !path.IsAbs(p) && path.Clean(p) != ".." && !strings.HasPrefix(path.Clean(p), "../")
}

func fileOverlap(a, b string) bool {
	a, b = path.Clean(a), path.Clean(b)
	return a == "." || b == "." || a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func overlaps(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}
