package publication

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PsyChaos/ai-team-harness/internal/snapshot"
)

type memoryHost struct {
	calls    []string
	evidence Evidence
	fail     string
	head     string
}

func (h *memoryHost) Publish(_ context.Context, req Request, _ Result) (PullRequest, error) {
	h.calls = append(h.calls, "publish")
	head := req.Head
	if h.head != "" {
		head = h.head
	}
	return PullRequest{42, head}, nil
}
func (h *memoryHost) PersistRouting(_ context.Context, e Evidence) error {
	h.calls = append(h.calls, "persist")
	h.evidence = e
	if h.fail == "persist" {
		return errors.New("storage failed")
	}
	return nil
}
func (h *memoryHost) AssignReview(_ context.Context, e Evidence, _ Result) error {
	h.calls = append(h.calls, "assign")
	if !reflect.DeepEqual(e, h.evidence) {
		return errors.New("unpersisted assignment")
	}
	return nil
}
func fixture(t *testing.T) (Service, Request, *memoryHost) {
	t.Helper()
	signer, err := snapshot.NewSigner(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := os.ReadFile("../../domain/testdata/bootstrap.json")
	if err != nil {
		t.Fatal(err)
	}
	graph, err = signer.Sign(graph)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/result.md")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	req := Request{Identity: Identity{"test/repo", 3, 2, "0123456789abcdef0123456789abcdef01234567"}, Branch: "ai/issue-3-publication", Graph: graph, Result: raw, Implementer: Session{"codex", "implementation-session"}, Now: now, Candidates: []Candidate{
		{Session{"claude", "implementation-session"}, true, now.Add(time.Hour)},
		{Session{"codex", "review-session-1"}, true, now.Add(time.Hour)},
		{Session{"gemini", "review-session-stale"}, true, now},
		{Session{"claude", "review-session-2"}, true, now.Add(time.Hour)},
	}}
	host := &memoryHost{}
	return Service{signer, host}, req, host
}
func TestReviewerDiversity(t *testing.T) {
	s, r, h := fixture(t)
	e, err := s.Publish(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if e.Reviewer.ID == r.Implementer.ID || e.Reviewer.Provider == r.Implementer.Provider || e.Reviewer.ID != "review-session-2" {
		t.Fatalf("non-independent reviewer: %+v", e)
	}
	if e.Identity != r.Identity || e.ImplementationDigest != digest(string(r.Result)) || !reflect.DeepEqual(h.calls, []string{"publish", "persist", "assign"}) {
		t.Fatalf("incorrect evidence/order: %+v %+v", e, h.calls)
	}
	r.Candidates = r.Candidates[:3]
	e, err = s.Publish(context.Background(), r)
	if err != nil || e.Reviewer.ID != "review-session-1" || !strings.Contains(e.Reason, "no different provider") {
		t.Fatalf("fallback: %+v %v", e, err)
	}
}
func TestRejectBeforePublication(t *testing.T) {
	for _, name := range []string{"unsigned", "tampered", "wrong-repository", "incomplete", "container", "cycle", "head", "schema", "no-reviewer"} {
		t.Run(name, func(t *testing.T) {
			s, r, h := fixture(t)
			var g map[string]any
			if err := json.Unmarshal(r.Graph, &g); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "unsigned":
				delete(g, "signature")
			case "tampered":
				g["publisher"] = "intruder"
			case "wrong-repository":
				r.Repo = "other/repo"
			case "incomplete":
				g["complete"] = false
			case "container":
				r.Issue = 2
			case "cycle":
				nodes := g["plan"].(map[string]any)["nodes"].([]any)
				nodes[1].(map[string]any)["depends_on"] = []string{"next"}
			case "head":
				r.Head = strings.Repeat("a", 40)
			case "schema":
				r.Result = []byte("not a result")
			case "no-reviewer":
				r.Candidates = r.Candidates[:1]
			}
			r.Graph, _ = json.Marshal(g)
			if name != "unsigned" && name != "tampered" {
				var err error
				r.Graph, err = s.Signer.Sign(r.Graph)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Publish(context.Background(), r); err == nil {
				t.Fatal("accepted invalid request")
			}
			if len(h.calls) != 0 {
				t.Fatalf("side effects before rejection: %v", h.calls)
			}
		})
	}
}
func TestPersistenceAndHeadFailurePreventDispatch(t *testing.T) {
	for _, which := range []string{"persist", "head"} {
		t.Run(which, func(t *testing.T) {
			s, r, h := fixture(t)
			if which == "persist" {
				h.fail = "persist"
			} else {
				h.head = strings.Repeat("f", 40)
			}
			if _, err := s.Publish(context.Background(), r); err == nil {
				t.Fatal("expected failure")
			}
			for _, c := range h.calls {
				if c == "assign" {
					t.Fatal("dispatched after failure")
				}
			}
		})
	}
}

// The current Python parser is the schema oracle, not a duplicated JSON schema.
func TestStructuredResultPythonParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/result.md")
	if err != nil {
		t.Fatal(err)
	}
	variants := map[string]string{
		"pending":           string(raw),
		"crlf":              strings.ReplaceAll(string(raw), "\n", "\r\n"),
		"success":           strings.ReplaceAll(strings.ReplaceAll(string(raw), "VALIDATION_PENDING", "SUCCESS"), "- [ ] [external:ci] Hosted CI pending.", "- [x] All required checks complete."),
		"unchecked":         strings.ReplaceAll(string(raw), "- [x] Local fixtures pass.", "- [ ] Local fixtures pass."),
		"no-pending":        strings.ReplaceAll(string(raw), "- [ ] [external:ci] Hosted CI pending.", ""),
		"two-shas":          strings.ReplaceAll(string(raw), "## Files Changed", strings.Repeat("b", 40)+"\n\n## Files Changed"),
		"duplicate-outcome": string(raw) + "\n## Outcome\nSUCCESS\n",
		"process-failure":   string(raw) + "\n# Harness Process Failure\n",
		"failed":            strings.ReplaceAll(string(raw), "VALIDATION_PENDING", "FAILED"),
	}
	broker, err := filepath.Abs("../../../.ai-team/coordinator/broker.py")
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range variants {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "result.md")
			if err := os.WriteFile(file, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			script := `import importlib.util,json,pathlib,sys
spec=importlib.util.spec_from_file_location("broker",sys.argv[1])
m=importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
try: print(json.dumps(m.parse_implementation_result(pathlib.Path(sys.argv[2]))))
except m.BrokerError: print("null")
`
			out, err := exec.Command("python3", "-c", script, broker, file).CombinedOutput()
			if err != nil {
				t.Fatalf("Python schema oracle: %v %s", err, out)
			}
			got, goErr := ParseResult([]byte(content))
			if strings.TrimSpace(string(out)) == "null" {
				if goErr == nil {
					t.Fatal("Go accepted Python-rejected result")
				}
				return
			}
			if goErr != nil {
				t.Fatal(goErr)
			}
			var want Result
			if err = json.Unmarshal(out, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Go: %+v Python: %+v", got, want)
			}
		})
	}
}
