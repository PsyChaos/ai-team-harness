package pybridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/PsyChaos/ai-team-harness/internal/domain"
	"github.com/PsyChaos/ai-team-harness/internal/journal"
)

// This source exposes reads only; it replays actual normalized Python output.
type recordedReader struct {
	items []domain.ProjectItem
	err   error
}

func (r recordedReader) ProjectItems(context.Context) ([]domain.ProjectItem, error) {
	return r.items, r.err
}

const snapshotFixture = "../../domain/testdata/snapshot.json"

func fixtureReader(t *testing.T) recordedReader {
	t.Helper()
	var item domain.ProjectItem
	if err := ReadJSON("../../domain/testdata/project-item.json", &item); err != nil {
		t.Fatal(err)
	}
	return recordedReader{items: []domain.ProjectItem{item}}
}

func TestRecordedSnapshotToJournal(t *testing.T) {
	before, err := os.ReadFile(snapshotFixture)
	if err != nil {
		t.Fatal(err)
	}
	reader := fixtureReader(t)
	// Sensitive/unrecognized input fields must not leak into observations.
	reader.items[0].Content.Body = "SECRET_ISSUE_BODY"
	reader.items[0].FieldValues.Nodes = append(reader.items[0].FieldValues.Nodes,
		json.RawMessage(`{"text":"SECRET_METADATA","field":{"name":"Evidence"}}`))
	events, err := Observe(context.Background(), snapshotFixture, reader, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].ID != 1 || events[1].ID != 2 || events[0].Type != "pybridge.broker.observed" || events[1].Type != "pybridge.task.observed" {
		t.Fatalf("events: %+v", events)
	}
	var broker BrokerObservation
	if err := json.Unmarshal(events[0].Data, &broker); err != nil {
		t.Fatal(err)
	}
	if broker.Repo != "acme/widget" || broker.CreatedAt != 1700000000 || broker.ExpiresAt != 1700000300 || broker.ProposedActions != 9 {
		t.Fatalf("broker: %+v", broker)
	}
	var task TaskObservation
	if err := json.Unmarshal(events[1].Data, &task); err != nil {
		t.Fatal(err)
	}
	if task.Issue != reader.items[0].Content.Number || task.Status != "READY" || !task.DependenciesComplete || len(task.Dependencies) != 1 || task.Dependencies[0].State != "CLOSED" {
		t.Fatalf("task: %+v", task)
	}
	encoded, _ := json.Marshal(events)
	for _, forbidden := range []string{"SECRET_", "signature", "nonce", "fingerprint", "a_9d003", "body", "origin_url", "dispatch_implementer"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("leaked %q", forbidden)
		}
	}
	path := filepath.Join(t.TempDir(), "journal.json")
	limits := journal.Limits{Retain: 8, Pending: 1, MaxEventBytes: 16384}
	j, err := journal.Open(path, limits)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if err := j.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := journal.Open(path, limits)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Snapshot(); got.Cursor != 2 || !reflect.DeepEqual(got.Events, events) {
		t.Fatalf("persisted: %+v", got)
	}
	next, err := Observe(context.Background(), snapshotFixture, reader, reopened.Snapshot().Cursor)
	if err != nil || next[0].ID != 3 {
		t.Fatalf("restart: %+v %v", next, err)
	}
	after, err := os.ReadFile(snapshotFixture)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("source snapshot changed", err)
	}
}

func TestReadFailuresProduceNoEvents(t *testing.T) {
	reader := fixtureReader(t)
	reader.err = errors.New("GitHub unavailable")
	if events, err := Observe(context.Background(), snapshotFixture, reader, 0); err == nil || events != nil {
		t.Fatal(events, err)
	}
	reader.err = nil
	reader.items[0].Content.Repository.NameWithOwner = "foreign/repo"
	if events, err := Observe(context.Background(), snapshotFixture, reader, 0); err == nil || events != nil {
		t.Fatal(events, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if events, err := Observe(ctx, snapshotFixture, fixtureReader(t), 0); !errors.Is(err, context.Canceled) || events != nil {
		t.Fatal(events, err)
	}
	if events, err := Observe(context.Background(), snapshotFixture, fixtureReader(t), ^uint64(0)-1); err == nil || events != nil {
		t.Fatal(events, err)
	}
}

func TestInputValidationAndIncompleteDependencies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	for _, bad := range []string{`null`, `{}`, `{"version":2}`, `{} {}`, `{"version":1,"repo":"acme/widget","created_at":1,"expires_at":2}`} {
		if err := os.WriteFile(path, []byte(bad), 0400); err != nil {
			t.Fatal(err)
		}
		if events, err := Observe(context.Background(), path, fixtureReader(t), 0); err == nil || events != nil {
			t.Fatal(bad, events, err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	reader := fixtureReader(t)
	reader.items[0].Content.BlockedBy = domain.Blockers{Complete: false, Nodes: []domain.Dependency{}}
	events, err := Observe(context.Background(), snapshotFixture, reader, 0)
	if err != nil {
		t.Fatal(err)
	}
	var task TaskObservation
	if err := json.Unmarshal(events[1].Data, &task); err != nil {
		t.Fatal(err)
	}
	if task.DependenciesComplete || task.Dependencies == nil {
		t.Fatalf("incomplete dependency read misrepresented: %+v", task)
	}
	reader.items[0].Content.Type = "DraftIssue"
	events, err = Observe(context.Background(), snapshotFixture, reader, 0)
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
}
