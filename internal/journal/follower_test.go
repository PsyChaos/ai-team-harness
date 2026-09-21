package journal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadOnlyFollower(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	limits := Limits{Retain: 3, Pending: 1, MaxEventBytes: 1024}
	producer, err := Open(path, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err = producer.Append(Event{ID: 1, Type: "observed", Data: json.RawMessage(`{"status":"unknown"}`)}); err != nil {
		t.Fatal(err)
	}
	follower, err := Open(path, limits)
	if err != nil {
		t.Fatal(err)
	}
	_, changed, err := follower.ReadAfter(1)
	if err != nil {
		t.Fatal(err)
	}
	if err = producer.Append(Event{ID: 2, Type: "observed", Data: json.RawMessage(`{"status":"running"}`)}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err = follower.Reload(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("follower wrote source")
	}
	select {
	case <-changed:
	default:
		t.Fatal("stream not notified")
	}
	events, _, err := follower.ReadAfter(1)
	if err != nil || len(events) != 1 || events[0].ID != 2 {
		t.Fatalf("followed events: %v %v", events, err)
	}
	_, unchanged, _ := follower.ReadAfter(2)
	if err = follower.Reload(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-unchanged:
		t.Fatal("unchanged source woke stream")
	default:
	}
	rewritten := []byte(`{"version":1,"cursor":2,"events":[{"id":1,"type":"observed","data":{"status":"changed"}},{"id":2,"type":"observed","data":{"status":"running"}}],"pending":[]}`)
	if err = os.WriteFile(path, rewritten, 0600); err != nil {
		t.Fatal(err)
	}
	if err = follower.Reload(); err == nil {
		t.Fatal("accepted rewritten event")
	}
	if err = os.WriteFile(path, []byte(`{"version":1,"cursor":0,"events":[],"pending":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = follower.Reload(); err == nil {
		t.Fatal("accepted backwards cursor")
	}
	if err = os.WriteFile(path, []byte(`invalid`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = follower.Reload(); err == nil {
		t.Fatal("accepted corrupt source")
	}
	if follower.Snapshot().Cursor != 2 {
		t.Fatal("lost retained observations on read failure")
	}
}
