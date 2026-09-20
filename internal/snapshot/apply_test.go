package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestApplyPreflightAndDispatch(t *testing.T) {
	s := signer(t)
	b := fixture(t, "snapshot")
	now := time.Unix(1700000000, 0)
	m, _ := decode(b)
	actions := m["actions"].([]any)
	ids := []any{}
	handlers := map[string]Handler{}
	got := []string{}
	for _, v := range actions {
		a := v.(map[string]any)
		ids = append(ids, a["id"])
		handlers[a["kind"].(string)] = func(_ context.Context, b json.RawMessage) error {
			a, e := decode(b)
			if e != nil {
				return e
			}
			got = append(got, a["id"].(string))
			return nil
		}
	}
	decisions := func(ids []any) []byte { return canonical(map[string]any{"selected_action_ids": ids}) }
	count, e := s.Apply(context.Background(), b, decisions(ids), "acme/widget", now, handlers)
	if e != nil || count != len(ids) {
		t.Fatal(count, e)
	}
	for i, id := range ids {
		if got[i] != id {
			t.Fatal("selection order changed")
		}
	}
	cases := [][]byte{decisions(append(append([]any{}, ids...), "unknown")), decisions([]any{ids[0], ids[0]}), decisions([]any{true}), []byte(`{"selected_action_ids":[],"extra":true}`), []byte(`{"selected_action_ids":null}`)}
	for _, d := range cases {
		got = nil
		n, e := s.Apply(context.Background(), b, d, "acme/widget", now, handlers)
		if e == nil || n != 0 || len(got) != 0 {
			t.Fatal("invalid selection caused side effects")
		}
	}
	got = nil
	incomplete := map[string]Handler{actions[0].(map[string]any)["kind"].(string): handlers[actions[0].(map[string]any)["kind"].(string)]}
	if _, e = s.Apply(context.Background(), b, decisions(ids), "acme/widget", now, incomplete); e == nil || len(got) != 0 {
		t.Fatal("missing handler caused partial execution")
	}
	// A failed handler must stop later actions and report only completed actions.
	failure := errors.New("state changed")
	calls := 0
	for k := range handlers {
		handlers[k] = func(context.Context, json.RawMessage) error {
			calls++
			if calls == 2 {
				return failure
			}
			return nil
		}
	}
	n, e := s.Apply(context.Background(), b, decisions(ids), "acme/widget", now, handlers)
	if n != 1 || calls != 2 || !errors.Is(e, failure) {
		t.Fatal(n, calls, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls = 0
	n, e = s.Apply(ctx, b, decisions(ids), "acme/widget", now, handlers)
	if n != 0 || calls != 0 || !errors.Is(e, context.Canceled) {
		t.Fatal(n, calls, e)
	}
}
func TestSnapshotRejection(t *testing.T) {
	s := signer(t)
	b := fixture(t, "snapshot")
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name, repo string
		now        time.Time
	}{{"wrong repo", "wrong/repo", now}, {"expired", "acme/widget", now.Add(301 * time.Second)}, {"future", "acme/widget", now.Add(-6 * time.Second)}} {
		t.Run(tc.name, func(t *testing.T) {
			if s.Validate(b, tc.repo, tc.now) == nil {
				t.Fatal("accepted invalid snapshot")
			}
		})
	}
	// Inclusive Python boundaries.
	for _, offset := range []int64{-5, 300} {
		if e := s.Validate(b, "acme/widget", now.Add(time.Duration(offset)*time.Second)); e != nil {
			t.Fatal(e)
		}
	}
	for _, change := range []string{"action payload", "action duplicate", "action kind", "nonce", "actions type", "timestamp bool", "version"} {
		t.Run(change, func(t *testing.T) {
			m, _ := decode(b)
			a := m["actions"].([]any)
			switch change {
			case "action payload":
				a[0].(map[string]any)["issue"] = json.Number("999")
			case "action duplicate":
				m["actions"] = append(a, a[0])
			case "action kind":
				a[0].(map[string]any)["kind"] = "unknown"
			case "nonce":
				m["nonce"] = "changed"
			case "actions type":
				m["actions"] = nil
			case "timestamp bool":
				m["created_at"] = true
			case "version":
				m["version"] = json.Number("2")
			}
			signed, e := s.Sign(canonical(m))
			if e != nil {
				t.Fatal(e)
			}
			if s.Validate(signed, "acme/widget", now) == nil {
				t.Fatal("accepted signed invalid snapshot")
			}
			n, e := s.Apply(context.Background(), signed, []byte(`{"selected_action_ids":[]}`), "acme/widget", now, nil)
			if n != 0 || e == nil {
				t.Fatal("apply accepted invalid snapshot")
			}
		})
	}
	if _, e := s.Apply(context.Background(), []byte(`{}`), []byte(`{"selected_action_ids":[]}`), "acme/widget", now, nil); e == nil {
		t.Fatal("apply accepted unsigned input")
	}
}

func TestNativeSigningMatchesPythonActionIDs(t *testing.T) {
	s := signer(t)
	b := fixture(t, "snapshot")
	m, _ := decode(b)
	expected := canonical(m)
	delete(m, "signature")
	for _, v := range m["actions"].([]any) {
		delete(v.(map[string]any), "id")
	}
	signed, e := s.SignSnapshot(canonical(m), "acme/widget", time.Unix(1700000000, 0))
	if e != nil {
		t.Fatal(e)
	}
	if string(signed) != string(expected) {
		t.Fatal("native action IDs or signature differ from Python")
	}
	var zero Signer
	if _, e := zero.Sign([]byte(`{}`)); e == nil {
		t.Fatal("zero-value signer accepted")
	}
}
