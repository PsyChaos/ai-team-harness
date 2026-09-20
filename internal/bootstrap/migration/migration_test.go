package migration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PsyChaos/ai-team-harness/internal/snapshot"
)

func recordedBody(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/graph.json")
	if err != nil {
		t.Fatal(err)
	}
	var graph struct {
		Issues []struct {
			Body string `json:"body"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(b, &graph); err != nil {
		t.Fatal(err)
	}
	return []byte(graph.Issues[0].Body)
}

func TestPreserveRecordedGraph(t *testing.T) {
	body := recordedBody(t)
	signer, _ := snapshot.NewSigner(strings.Repeat("ab", 32))
	got, err := Preserve(body, "test/repo", signer)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("preservation failed", err)
	}
	again, err := Preserve(got, "TEST/REPO", signer)
	if err != nil || !bytes.Equal(got, again) {
		t.Fatal("not idempotent", err)
	}
	got[0] = 'x'
	if body[0] == 'x' {
		t.Fatal("returned alias")
	}
	for name, input := range map[string][]byte{
		"tampered":         bytes.Replace(body, []byte("Build a useful product"), []byte("changed"), 1),
		"duplicate marker": append(bytes.Clone(body), []byte(PlanBegin)...),
		"missing marker":   bytes.Replace(body, []byte(PlanEnd), nil, 1),
		"root identity":    bytes.Replace(body, []byte("474bde522a1fb0f8e5b73314"), []byte(strings.Repeat("0", 24)), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if out, err := Preserve(input, "test/repo", signer); err == nil || out != nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
	if _, err := Preserve(body, "other/repo", signer); err == nil {
		t.Fatal("wrong repo accepted")
	}
	if _, err := Preserve(body, "test/repo", nil); err == nil {
		t.Fatal("missing signer accepted")
	}
}

func TestDocumentedRunbook(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "bootstrap-migrate")
	build := exec.Command("go", "build", "-o", binary, "./cmd/bootstrap-migrate")
	build.Dir = "../../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cmd := exec.Command("python3", "testdata/exercise.py", binary)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runbook: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
}

func TestPreserveIncompleteAndExtendedEnvelope(t *testing.T) {
	body := recordedBody(t)
	start := bytes.Index(body, []byte(PlanBegin)) + len(PlanBegin)
	end := bytes.Index(body, []byte(PlanEnd))
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body[start:end], &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["complete"] = json.RawMessage(`false`)
	envelope["extension"] = json.RawMessage(`{"integer":9007199254740993,"null":null,"empty":[],"unicode":"é😀"}`)
	unsigned, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := snapshot.NewSigner(strings.Repeat("ab", 32))
	signed, err := signer.Sign(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	input := append(bytes.Clone(body[:start]), signed...)
	input = append(input, body[end:]...)
	input = bytes.Replace(input, []byte("\n\n<!-- ai-harness-bootstrap:complete -->\n"), nil, 1)
	got, err := Preserve(input, "test/repo", signer)
	if err != nil || !bytes.Equal(got, input) {
		t.Fatal("incomplete graph or extensions changed", err)
	}
}
