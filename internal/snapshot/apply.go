package snapshot

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var allowed = map[string]bool{
	"dispatch_implementer": true, "reconcile_implementation": true, "publish_pr": true,
	"dispatch_review": true, "consume_review": true, "retry_implementation": true,
	"advance_merge_gate": true, "merge_pr": true, "finalize_merge": true, "unlock_dependency": true,
}

// Handler must re-read authoritative state and enforce the lifecycle action's
// existing policy, freshness and concurrency gates before any side effect.
// Apply provides signature/selection validation, not those execution gates.
type Handler func(context.Context, json.RawMessage) error

// Validate checks the broker's signature, identity, time window and action IDs.
func (s *Signer) Validate(data []byte, repo string, now time.Time) error {
	_, e := s.validated(data, repo, now)
	return e
}
func (s *Signer) validated(data []byte, repo string, now time.Time) (map[string]any, error) {
	m, e := s.verify(data)
	if e != nil {
		return nil, e
	}
	if repo == "" || m["repo"] != repo || m["version"] != json.Number("1") {
		return nil, errors.New("snapshot identity mismatch")
	}
	integer := func(k string) (int64, error) {
		n, ok := m[k].(json.Number)
		if !ok {
			return 0, errors.New("invalid timestamp")
		}
		return n.Int64()
	}
	expires, e := integer("expires_at")
	if e != nil || now.Unix() > expires {
		return nil, errors.New("snapshot is stale or expiry invalid")
	}
	created, e := integer("created_at")
	if e != nil || created > now.Unix()+5 {
		return nil, errors.New("snapshot timestamp is invalid")
	}
	nonce, ok := m["nonce"].(string)
	if !ok || nonce == "" {
		return nil, errors.New("invalid nonce")
	}
	actions, ok := m["actions"].([]any)
	if !ok {
		return nil, errors.New("invalid actions")
	}
	seen := map[string]bool{}
	for _, v := range actions {
		a, ok := v.(map[string]any)
		if !ok {
			return nil, errors.New("invalid action")
		}
		kind, ok := a["kind"].(string)
		if !ok || !allowed[kind] {
			return nil, errors.New("invalid action kind")
		}
		id, ok := a["id"].(string)
		payload := map[string]any{}
		for k, v := range a {
			if k != "id" {
				payload[k] = v
			}
		}
		expected := "a_" + s.digest(map[string]any{"nonce": nonce, "payload": payload})
		if !ok || !hmac.Equal([]byte(id), []byte(expected)) || seen[id] {
			return nil, errors.New("invalid or duplicate action ID")
		}
		seen[id] = true
	}
	return m, nil
}

// Apply preflights the entire snapshot, decision document and handler set before
// dispatch. It preserves selection order and stops on the first handler error,
// returning the count completed. It is not transactional or replay protection;
// the caller must serialize execution and handlers must revalidate current state.
func (s *Signer) Apply(ctx context.Context, data, decisions []byte, repo string, now time.Time, handlers map[string]Handler) (int, error) {
	m, e := s.validated(data, repo, now)
	if e != nil {
		return 0, e
	}
	d, e := decode(decisions)
	if e != nil {
		return 0, e
	}
	ids, ok := d["selected_action_ids"].([]any)
	if len(d) != 1 || !ok {
		return 0, errors.New("decisions must contain only selected_action_ids")
	}
	indexed := map[string]map[string]any{}
	for _, v := range m["actions"].([]any) {
		a := v.(map[string]any)
		indexed[a["id"].(string)] = a
	}
	type call struct {
		handler Handler
		data    json.RawMessage
	}
	calls := []call{}
	seen := map[string]bool{}
	for _, v := range ids {
		id, ok := v.(string)
		if !ok || seen[id] {
			return 0, errors.New("invalid selected action ID")
		}
		a, ok := indexed[id]
		if !ok {
			return 0, errors.New("unknown selected action ID")
		}
		seen[id] = true
		handler := handlers[a["kind"].(string)]
		if handler == nil {
			return 0, errors.New("missing lifecycle handler")
		}
		calls = append(calls, call{handler, canonical(a)})
	}
	for i, c := range calls {
		if e := ctx.Err(); e != nil {
			return i, e
		}
		if e := c.handler(ctx, c.data); e != nil {
			return i, fmt.Errorf("action %d: %w", i, e)
		}
	}
	return len(calls), nil
}

// SignSnapshot creates Python-compatible action IDs and an envelope signature
// for trusted producer data, then checks the resulting snapshot. As with Sign,
// this must never be exposed as an approval path for untrusted supplied plans.
func (s *Signer) SignSnapshot(data []byte, repo string, now time.Time) ([]byte, error) {
	if s == nil || !s.configured {
		return nil, errors.New("missing signer")
	}
	m, e := decode(data)
	if e != nil {
		return nil, e
	}
	nonce, ok := m["nonce"].(string)
	if !ok || nonce == "" {
		return nil, errors.New("invalid nonce")
	}
	actions, ok := m["actions"].([]any)
	if !ok {
		return nil, errors.New("invalid actions")
	}
	for _, v := range actions {
		a, ok := v.(map[string]any)
		if !ok {
			return nil, errors.New("invalid action")
		}
		delete(a, "id")
		id := "a_" + s.digest(map[string]any{"nonce": nonce, "payload": a})
		a["id"] = id
	}
	signed, e := s.Sign(canonical(m))
	if e != nil {
		return nil, e
	}
	if e = s.Validate(signed, repo, now); e != nil {
		return nil, e
	}
	return signed, nil
}
