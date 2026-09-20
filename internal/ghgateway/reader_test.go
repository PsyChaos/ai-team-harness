package ghgateway

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/PsyChaos/ai-team-harness/internal/domain"
)

type recordedCall struct {
	Operation string          `json:"operation"`
	Variables map[string]any  `json:"variables"`
	Response  json.RawMessage `json:"response"`
}
type recording struct {
	Calls []recordedCall `json:"calls"`
	t     *testing.T
}

func loadRecording(t *testing.T) *recording {
	t.Helper()
	data, err := os.ReadFile("testdata/responses.json")
	if err != nil {
		t.Fatal(err)
	}
	r := &recording{t: t}
	if err := json.Unmarshal(data, r); err != nil {
		t.Fatal(err)
	}
	return r
}
func (r *recording) next(operation string, variables map[string]any) json.RawMessage {
	r.t.Helper()
	if len(r.Calls) == 0 {
		r.t.Fatal("unexpected source request")
	}
	call := r.Calls[0]
	r.Calls = r.Calls[1:]
	data, _ := json.Marshal(variables)
	var normalized map[string]any
	_ = json.Unmarshal(data, &normalized)
	if operation != call.Operation || !reflect.DeepEqual(normalized, call.Variables) {
		r.t.Fatalf("request mismatch: %s %s; want %+v", operation, data, call)
	}
	return call.Response
}
func (r *recording) OwnerType(_ context.Context, owner string) (string, error) {
	if owner != "acme" {
		r.t.Fatalf("owner: %s", owner)
	}
	var value struct {
		Type string `json:"type"`
	}
	err := json.Unmarshal(r.next("owner", map[string]any{}), &value)
	return value.Type, err
}
func (r *recording) GraphQL(_ context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	operation := "HarnessProjectItems"
	if strings.Contains(query, "query HarnessProjectBlockers") {
		operation = "HarnessProjectBlockers"
	}
	return r.next(operation, variables), nil
}
func gateway(source Source) *Gateway {
	return &Gateway{Source: source, Repo: "acme/widget", Owner: "acme", Number: 1}
}

func TestParity(t *testing.T) {
	r := loadRecording(t)
	items, err := gateway(r).ProjectItems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Calls) != 0 {
		t.Fatal("unused recorded calls")
	}
	actual, err := json.Marshal(Reconstruct(items))
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/python-state.json")
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err = json.Unmarshal(golden, &want); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(actual, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("Go state differs from Python capture:\n%s", actual)
	}
	if path := os.Getenv("GHGATEWAY_PARITY_OUTPUT"); path != "" {
		if err := os.WriteFile(path, actual, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

type fakeSource struct {
	owner    string
	response json.RawMessage
	err      error
	calls    int
	queries  []string
}

func (s *fakeSource) OwnerType(context.Context, string) (string, error) { return s.owner, s.err }
func (s *fakeSource) GraphQL(_ context.Context, q string, _ map[string]any) (json.RawMessage, error) {
	s.calls++
	s.queries = append(s.queries, q)
	return s.response, s.err
}

func TestReadBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, owner, response string
		wantCalls             int
	}{
		{"bad owner", "Bot", `{}`, 0},
		{"graphql errors", "User", `{"data":{"user":{"projectV2":{"items":{"nodes":[]}}}},"errors":[{"message":"denied"}]}`, 1},
		{"missing identity", "User", `{"data":{"user":null}}`, 1},
		{"missing nodes", "User", `{"data":{"user":{"projectV2":{"items":{}}}}}`, 1},
		{"missing cursor", "User", `{"data":{"user":{"projectV2":{"items":{"nodes":[],"pageInfo":{"hasNextPage":true}}}}}}`, 1},
		{"bounded pages", "User", `{"data":{"user":{"projectV2":{"items":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"again"}}}}}}`, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &fakeSource{owner: tc.owner, response: json.RawMessage(tc.response)}
			items, err := gateway(source).ProjectItems(context.Background())
			if err == nil || items != nil || source.calls != tc.wantCalls {
				t.Fatalf("items=%v err=%v calls=%d", items, err, source.calls)
			}
		})
	}
	source := &fakeSource{owner: "User", response: json.RawMessage(`{"data":{"user":{"projectV2":{"items":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`)}
	if _, err := gateway(source).ProjectItems(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source.queries[0], "user(login:") {
		t.Fatal("incorrect owner query")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gateway(source).ProjectItems(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	source.err = errors.New("offline failure")
	if _, err := gateway(source).ProjectItems(context.Background()); !errors.Is(err, source.err) {
		t.Fatal(err)
	}
}

func TestRejectInvalidItems(t *testing.T) {
	r := loadRecording(t)
	var page map[string]any
	_ = json.Unmarshal(r.Calls[1].Response, &page)
	node := page["data"].(map[string]any)["organization"].(map[string]any)["projectV2"].(map[string]any)["items"].(map[string]any)["nodes"].([]any)[0]
	original, _ := json.Marshal(node)
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"archive unknown", func(v map[string]any) { delete(v, "isArchived") }},
		{"archived", func(v map[string]any) { v["isArchived"] = true }},
		{"truncated fields", func(v map[string]any) {
			v["fieldValues"].(map[string]any)["pageInfo"] = map[string]any{"hasNextPage": true}
		}},
		{"foreign issue", func(v map[string]any) {
			v["content"].(map[string]any)["repository"] = map[string]any{"nameWithOwner": "other/repo"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var v map[string]any
			_ = json.Unmarshal(original, &v)
			tc.edit(v)
			raw, _ := json.Marshal(v)
			if _, err := normalizeItem(raw, "acme/widget"); err == nil {
				t.Fatal("accepted invalid item")
			}
		})
	}
}

func TestDependencyValidation(t *testing.T) {
	for _, raw := range []string{
		`null`, `{"id":"wrong","blockedBy":{"nodes":[],"pageInfo":{}}}`,
		`{"id":"I","blockedBy":{"nodes":[]}}`,
		`{"id":"I","blockedBy":{"nodes":[{"number":1,"url":"u","title":"t","state":"UNKNOWN","repository":{}}],"pageInfo":{}}}`,
		`{"id":"I","blockedBy":{"nodes":[{"number":1,"url":"u","state":"CLOSED","repository":{}}],"pageInfo":{}}}`,
	} {
		if _, err := normalizeBlockers(json.RawMessage(raw), "I"); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestFieldValue(t *testing.T) {
	for _, tc := range []struct{ node, want string }{
		{`{"field":{"name":"Retry Count"},"number":0,"text":"ignored"}`, "0"},
		{`{"field":{"name":"Retry Count"},"number":2.0}`, "2"},
		{`{"field":{"name":"Retry Count"},"number":1.5}`, "1.5"},
		{`{"field":{"name":"Retry Count"},"text":"2"}`, "2"},
		{`{"field":{"name":"Other"},"number":2}`, ""},
	} {
		item := domain.ProjectItem{FieldValues: domain.FieldValues{Nodes: []json.RawMessage{json.RawMessage(tc.node)}}}
		if got := FieldValue(item, "Retry Count"); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.node, got, tc.want)
		}
	}
}

// batchSource verifies the native API boundary for more than one hundred blockers.
type batchSource struct {
	t     *testing.T
	sizes []int
	mode  string
}

func (s *batchSource) OwnerType(context.Context, string) (string, error) { return "Organization", nil }
func (s *batchSource) GraphQL(_ context.Context, query string, vars map[string]any) (json.RawMessage, error) {
	if !strings.Contains(query, "query HarnessProjectBlockers") {
		s.t.Fatal("unexpected query")
	}
	ids := vars["ids"].([]string)
	s.sizes = append(s.sizes, len(ids))
	nodes := []any{}
	for _, id := range ids {
		nodes = append(nodes, map[string]any{"id": id, "blockedBy": map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}}})
	}
	switch s.mode {
	case "short":
		nodes = nodes[:len(nodes)-1]
	case "wrong identity":
		nodes[0].(map[string]any)["id"] = "wrong"
	case "transport":
		return nil, errors.New("dependency read failed")
	}
	raw, err := json.Marshal(map[string]any{"data": map[string]any{"nodes": nodes}})
	return raw, err
}
func TestDependencyBatching(t *testing.T) {
	for _, mode := range []string{"", "short", "wrong identity", "transport"} {
		t.Run(mode, func(t *testing.T) {
			items := make([]domain.ProjectItem, 101)
			for i := range items {
				items[i].Content = domain.Issue{ID: strconv.Itoa(i), Type: "Issue"}
				items[i].FieldValues.Nodes = []json.RawMessage{json.RawMessage(`{"field":{"name":"Harness Status"},"name":"BLOCKED"}`)}
			}
			source := &batchSource{t: t, mode: mode}
			err := gateway(source).hydrate(context.Background(), items)
			if mode != "" {
				if err == nil {
					t.Fatal("accepted failed dependency batch")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(source.sizes, []int{100, 1}) {
				t.Fatal(source.sizes)
			}
			for _, item := range items {
				if !item.Content.BlockedBy.Complete {
					t.Fatal("missing hydration")
				}
			}
		})
	}
}
