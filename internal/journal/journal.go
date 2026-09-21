package journal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrCursor   = errors.New("cursor outside retained window; fetch snapshot")
	ErrCapacity = errors.New("reorder window full")
)

// Event IDs are immutable, consecutive source sequence numbers starting at one.
// Data must contain only dashboard-safe observation data, never credentials.
type Event struct {
	ID   uint64          `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type Limits struct {
	Retain        int
	Pending       int
	MaxEventBytes int
}

// Snapshot describes the retained observation window, not authoritative work state.
type Snapshot struct {
	Version int     `json:"version"`
	Cursor  uint64  `json:"cursor"`
	Events  []Event `json:"events"`
}

type diskState struct {
	Snapshot
	Pending []Event `json:"pending"`
}

type Journal struct {
	mu      sync.Mutex
	path    string
	limits  Limits
	state   diskState
	changed chan struct{}
}

// Open loads a journal owned exclusively by this instance. Use a private directory.
// Limits must remain identical across restarts (or accommodate all stored records).
func Open(path string, limits Limits) (*Journal, error) {
	if limits.Retain < 1 || limits.Pending < 1 || limits.MaxEventBytes < 64 || limits.MaxEventBytes > 1<<20 || limits.Retain > 1<<20 || limits.Pending > 1<<20 {
		return nil, errors.New("invalid journal limits")
	}
	// Bound decoding, including corrupt or unexpectedly large persisted input.
	max := int64(limits.Retain) + int64(limits.Pending)
	if max > (1<<30)/int64(limits.MaxEventBytes+128) {
		return nil, errors.New("journal limits too large")
	}
	bound := max*int64(limits.MaxEventBytes+128) + 1024
	j := &Journal{path: path, limits: limits, state: diskState{Snapshot: Snapshot{Version: 1, Events: []Event{}}}, changed: make(chan struct{})}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > bound {
		return nil, errors.New("journal file exceeds limits")
	}
	dec := json.NewDecoder(io.LimitReader(f, bound))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&j.state); err != nil {
		return nil, err
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("trailing journal data")
	}
	if err = j.validate(); err != nil {
		return nil, err
	}
	return j, nil
}

func (j *Journal) validEvent(e Event) bool {
	if len(e.Data) > j.limits.MaxEventBytes || e.ID == 0 || e.Type == "" || len(e.Type) > 128 || !json.Valid(e.Data) {
		return false
	}
	b, err := json.Marshal(e)
	return err == nil && len(b) <= j.limits.MaxEventBytes
}

func (j *Journal) validate() error {
	s := j.state
	if s.Version != 1 || len(s.Events) > j.limits.Retain || len(s.Pending) > j.limits.Pending || uint64(len(s.Events)) > s.Cursor || (s.Cursor > 0 && len(s.Events) == 0) {
		return errors.New("invalid journal state")
	}
	for i, e := range s.Events {
		if !j.validEvent(e) || e.ID != s.Cursor-uint64(len(s.Events))+uint64(i)+1 {
			return errors.New("invalid retained sequence")
		}
	}
	seen := map[uint64]bool{}
	for _, e := range s.Pending {
		if !j.validEvent(e) || e.ID <= s.Cursor || e.ID-s.Cursor <= 1 || seen[e.ID] {
			return errors.New("invalid pending sequence")
		}
		seen[e.ID] = true
	}
	return nil
}

// Append persists before publishing. Repeated IDs are ignored: the first payload
// wins. Gaps are buffered up to Pending records; callers retry rejected records.
func (j *Journal) Append(e Event) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.validEvent(e) {
		return errors.New("invalid or oversized event")
	}
	if e.ID <= j.state.Cursor {
		return nil
	}
	for _, p := range j.state.Pending {
		if p.ID == e.ID {
			return nil
		}
	}
	if e.ID != j.state.Cursor+1 && len(j.state.Pending) >= j.limits.Pending {
		return ErrCapacity
	}
	// Own caller data and build a replacement, leaving current state intact on failure.
	e.Data = append(json.RawMessage(nil), e.Data...)
	next := diskState{Snapshot: Snapshot{Version: 1, Cursor: j.state.Cursor, Events: append([]Event{}, j.state.Events...)}, Pending: append([]Event{}, j.state.Pending...)}
	next.Pending = append(next.Pending, e)
	for {
		found := -1
		for i, p := range next.Pending {
			if p.ID == next.Cursor+1 {
				found = i
				break
			}
		}
		if found < 0 {
			break
		}
		next.Events = append(next.Events, next.Pending[found])
		next.Cursor++
		next.Pending = append(next.Pending[:found], next.Pending[found+1:]...)
	}
	if len(next.Events) > j.limits.Retain {
		next.Events = append([]Event{}, next.Events[len(next.Events)-j.limits.Retain:]...)
	}
	if err := j.persist(next); err != nil {
		return err
	}
	j.state = next
	close(j.changed)
	j.changed = make(chan struct{})
	return nil
}

func (j *Journal) persist(s diskState) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(j.path), ".journal-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), j.path); err != nil {
		return fmt.Errorf("replace journal: %w", err)
	}
	return nil
}

func cloneEvents(events []Event) []Event {
	out := make([]Event, len(events))
	for i, e := range events {
		out[i] = e
		out[i].Data = append(json.RawMessage(nil), e.Data...)
	}
	return out
}

func (j *Journal) Snapshot() Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return Snapshot{Version: 1, Cursor: j.state.Cursor, Events: cloneEvents(j.state.Events)}
}

// ReadAfter atomically captures events and a change notification, avoiding lost wakeups.
func (j *Journal) ReadAfter(cursor uint64) ([]Event, <-chan struct{}, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if cursor > j.state.Cursor || cursor < j.state.Cursor-uint64(len(j.state.Events)) {
		return nil, nil, ErrCursor
	}
	offset := int(cursor - (j.state.Cursor - uint64(len(j.state.Events))))
	return cloneEvents(j.state.Events[offset:]), j.changed, nil
}

// Reload refreshes a read-only follower from its configured journal file. The
// follower must never Append: one external producer owns persistence. Replaced
// histories must preserve immutable sequence IDs; truncation/rewrite is rejected.
func (j *Journal) Reload() error {
	next, err := Open(j.path, j.limits)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if next.state.Cursor < j.state.Cursor {
		return errors.New("follower source cursor moved backwards")
	}
	for _, old := range j.state.Events {
		for _, current := range next.state.Events {
			if old.ID == current.ID && (old.Type != current.Type || string(old.Data) != string(current.Data)) {
				return errors.New("follower source rewrote an immutable event")
			}
		}
	}
	before, _ := json.Marshal(j.state)
	after, _ := json.Marshal(next.state)
	if string(before) != string(after) {
		j.state = next.state
		close(j.changed)
		j.changed = make(chan struct{})
	}
	return nil
}
