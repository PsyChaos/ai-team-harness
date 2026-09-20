// Package migration implements a preservation-only transition to the Go HMAC
// reader. It does not publish, re-sign, repair graphs, or authorize dispatch.
package migration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/PsyChaos/ai-team-harness/internal/snapshot"
)

const (
	PlanBegin = "<!-- ai-harness-plan:v1 -->\n"
	PlanEnd   = "\n<!-- /ai-harness-plan -->"
)

var rootMarker = regexp.MustCompile(`^<!-- ai-harness-bootstrap:v1:([a-f0-9]{24}) -->\n`)

// Preserve verifies the existing signature and its repository/root binding,
// then returns an independent, byte-identical copy of the tracking issue body.
// Graph/schema/publisher/native-edge checks remain the executor's responsibility.
// Incomplete graphs are preserved as incomplete; this is not a completion gate.
func Preserve(body []byte, repo string, signer *snapshot.Signer) ([]byte, error) {
	if repo == "" || len(body) > 4*1024*1024 {
		return nil, errors.New("missing repository or oversized tracking body")
	}
	marker := rootMarker.FindSubmatch(body)
	if marker == nil || bytes.Count(body, []byte(PlanBegin)) != 1 || bytes.Count(body, []byte(PlanEnd)) != 1 {
		return nil, errors.New("missing or ambiguous bootstrap markers")
	}
	start := bytes.Index(body, []byte(PlanBegin)) + len(PlanBegin)
	end := bytes.Index(body, []byte(PlanEnd))
	if end < start {
		return nil, errors.New("invalid bootstrap marker order")
	}
	envelope := body[start:end]
	if err := signer.Verify(envelope); err != nil {
		return nil, fmt.Errorf("bootstrap verification: %w", err)
	}
	var identity struct {
		Repo        string `json:"repo"`
		BootstrapID string `json:"bootstrap_id"`
	}
	if err := json.Unmarshal(envelope, &identity); err != nil {
		return nil, err
	}
	if !strings.EqualFold(identity.Repo, repo) || identity.BootstrapID != string(marker[1]) {
		return nil, errors.New("bootstrap repository or root identity mismatch")
	}
	return bytes.Clone(body), nil
}
