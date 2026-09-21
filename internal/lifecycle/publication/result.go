package publication

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Result is the Python broker's parsed Markdown implementation evidence.
type Result struct {
	Commit            string   `json:"commit"`
	Digest            string   `json:"digest"`
	ValidationDigest  string   `json:"validation_digest"`
	PendingValidation []string `json:"pending_validation"`
}

func digest(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
func section(raw, name string, next ...string) (string, error) {
	marker := "## " + name + "\n"
	if strings.Count(raw, marker) != 1 {
		return "", fmt.Errorf("structured result missing unique %s", name)
	}
	rest := strings.SplitN(raw, marker, 2)[1]
	end := len(rest)
	for _, n := range next {
		if i := strings.Index(rest, "\n## "+n+"\n"); i >= 0 && i < end {
			end = i
		}
	}
	return strings.TrimSpace(rest[:end]), nil
}

var sha = regexp.MustCompile(`^[0-9a-f]{40}$`)
var shaRuns = regexp.MustCompile(`[0-9a-f]+`)
var checked = regexp.MustCompile(`^- \[[xX]\] .+`)
var pending = regexp.MustCompile(`^- \[ \] \[external:(ci|browser|host)\] .+`)

// ParseResult validates the existing broker schema, without interpreting prose as
// instructions or treating pending external checks as passing evidence.
func ParseResult(raw []byte) (Result, error) {
	r := Result{PendingValidation: []string{}}
	// Path.read_text in the Python broker performs universal newline conversion.
	s := strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n")
	if len(raw) == 0 || len(raw) > 2_000_000 || !utf8.Valid(raw) || !strings.HasPrefix(s, "# Implementation Result\n") || strings.Contains(s, "# Harness Process Failure") {
		return r, fmt.Errorf("invalid implementation result marker or size")
	}
	outcome, err := section(s, "Outcome", "Summary")
	if err != nil {
		return r, err
	}
	if outcome != "SUCCESS" && outcome != "VALIDATION_PENDING" {
		return r, fmt.Errorf("implementation outcome is not publishable")
	}
	commit, err := section(s, "Commit", "Files Changed", "Acceptance Criteria Mapping")
	if err != nil {
		return r, err
	}
	var commits []string
	for _, run := range shaRuns.FindAllString(commit, -1) {
		if len(run) == 40 {
			commits = append(commits, run)
		}
	}
	if len(commits) != 1 {
		return r, fmt.Errorf("requires exactly one full commit SHA")
	}
	acceptance, err := section(s, "Acceptance Criteria Mapping", "Validation")
	if err != nil {
		return r, err
	}
	count, complete := 0, 0
	for _, line := range strings.Split(acceptance, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-") {
			continue
		}
		count++
		if outcome == "VALIDATION_PENDING" && pending.MatchString(line) {
			r.PendingValidation = append(r.PendingValidation, line)
			continue
		}
		if !checked.MatchString(line) {
			return r, fmt.Errorf("acceptance criteria incomplete")
		}
		complete++
	}
	if count == 0 || outcome == "VALIDATION_PENDING" && (complete == 0 || len(r.PendingValidation) == 0) {
		return r, fmt.Errorf("acceptance criteria incomplete")
	}
	validation, err := section(s, "Validation", "Risks / Limitations", "Follow-ups")
	if err != nil {
		return r, err
	}
	if utf8.RuneCountInString(validation) < 8 {
		return r, fmt.Errorf("validation evidence empty")
	}
	r.Commit, r.Digest, r.ValidationDigest = commits[0], digest(s), digest(validation)
	return r, nil
}
