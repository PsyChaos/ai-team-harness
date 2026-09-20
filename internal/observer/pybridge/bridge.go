// Package pybridge is a temporary, migration-only observer of Python broker
// snapshots and GitHub read state. Remove it once native execution is active.
// It never verifies/executes signed actions or writes to either input source.
package pybridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/PsyChaos/ai-team-harness/internal/domain"
	"github.com/PsyChaos/ai-team-harness/internal/ghgateway"
	"github.com/PsyChaos/ai-team-harness/internal/journal"
)

const maxInputBytes = 16 << 20

// ReadJSON opens a bounded, regular input file read-only. Unknown fields are
// tolerated for Python compatibility but are never forwarded automatically.
func ReadJSON(path string, target any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() || stat.Size() > maxInputBytes {
		return errors.New("observer input must be a regular file at most 16 MiB")
	}
	dec := json.NewDecoder(io.LimitReader(f, maxInputBytes+1))
	if err := dec.Decode(target); err != nil {
		return err
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return errors.New("trailing observer input")
	}
	return nil
}

// BrokerObservation deliberately excludes IDs, signatures, nonces, digests,
// paths and action payloads. Counts describe proposals, never executed work.
type BrokerObservation struct {
	Repo            string `json:"repo"`
	CreatedAt       int64  `json:"created_at"`
	ExpiresAt       int64  `json:"expires_at"`
	ProposedActions int    `json:"proposed_actions"`
}

// TaskObservation contains only the dashboard read projection. No lease or
// liveness is inferred from a status. Dependency completeness remains explicit.
type TaskObservation struct {
	Repo                 string       `json:"repo"`
	Issue                int          `json:"issue"`
	State                string       `json:"state"`
	Status               string       `json:"status"`
	Provider             string       `json:"provider"`
	Role                 string       `json:"role"`
	RetryCount           string       `json:"retry_count"`
	DependenciesComplete bool         `json:"dependencies_complete"`
	Dependencies         []Dependency `json:"dependencies"`
}

type Dependency struct {
	Repo  string `json:"repo"`
	Issue int    `json:"issue"`
	State string `json:"state"`
}

// Observe constructs journal-compatible events after both reads succeed. The
// caller owns the journal and supplies its committed cursor. Use a dedicated
// single producer journal with no pending records. Repeated observations are
// new samples, not deduplicated lifecycle transitions. No source writes occur.
func Observe(ctx context.Context, snapshotPath string, github ghgateway.Reader, cursor uint64) ([]journal.Event, error) {
	var snapshot domain.Snapshot
	if err := ReadJSON(snapshotPath, &snapshot); err != nil {
		return nil, err
	}
	if snapshot.Version != 1 || snapshot.Repo == "" || snapshot.CreatedAt <= 0 || snapshot.ExpiresAt < snapshot.CreatedAt || snapshot.Actions == nil {
		return nil, errors.New("invalid broker snapshot")
	}
	if github == nil {
		return nil, errors.New("GitHub reader is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items, err := github.ProjectItems(ctx)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	events := []journal.Event{}
	add := func(kind string, value any) error {
		if cursor == ^uint64(0) {
			return errors.New("journal sequence exhausted")
		}
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		cursor++
		events = append(events, journal.Event{ID: cursor, Type: kind, Data: data})
		return nil
	}
	if err := add("pybridge.broker.observed", BrokerObservation{snapshot.Repo, snapshot.CreatedAt, snapshot.ExpiresAt, len(snapshot.Actions)}); err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.Content.Type != "Issue" {
			continue
		}
		if item.Content.Repository.NameWithOwner != snapshot.Repo || item.Content.Number <= 0 {
			return nil, errors.New("GitHub issue does not match broker repository")
		}
		task := TaskObservation{
			Repo: snapshot.Repo, Issue: item.Content.Number, State: item.Content.State,
			Status: ghgateway.FieldValue(item, "Harness Status"), Provider: ghgateway.FieldValue(item, "Provider"),
			Role: ghgateway.FieldValue(item, "Agent Role"), RetryCount: ghgateway.FieldValue(item, "Retry Count"),
			DependenciesComplete: item.Content.BlockedBy.Complete, Dependencies: []Dependency{},
		}
		for _, dep := range item.Content.BlockedBy.Nodes {
			repo := ""
			if dep.Repository != nil {
				repo = dep.Repository.NameWithOwner
			}
			task.Dependencies = append(task.Dependencies, Dependency{repo, dep.Number, dep.State})
		}
		if err := add("pybridge.task.observed", task); err != nil {
			return nil, err
		}
	}
	return events, nil
}
