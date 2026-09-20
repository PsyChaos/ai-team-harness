// Command pybridge is a temporary migration-only observation tool. Remove it
// when native execution is active. It has no broker or GitHub mutation commands.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/PsyChaos/ai-team-harness/internal/domain"
	"github.com/PsyChaos/ai-team-harness/internal/ghgateway"
	"github.com/PsyChaos/ai-team-harness/internal/journal"
	"github.com/PsyChaos/ai-team-harness/internal/observer/pybridge"
)

// Only Gateway's compiled-in read queries reach this private transport. No
// user-provided GraphQL or shell commands are accepted.
type ghSource struct{}

func (ghSource) OwnerType(ctx context.Context, owner string) (string, error) {
	b, err := exec.CommandContext(ctx, "gh", "api", "--method", "GET", "users/"+owner, "--jq", ".type").Output()
	if err != nil {
		return "", errors.New("GitHub owner read failed")
	}
	var ownerType string
	// gh --jq emits an unquoted string.
	_, err = fmt.Sscan(string(b), &ownerType)
	return ownerType, err
}
func (ghSource) GraphQL(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	args := []string{"api", "graphql", "-f", "query=" + query}
	for key, value := range variables {
		switch v := value.(type) {
		case string:
			args = append(args, "-f", key+"="+v)
		case int:
			args = append(args, "-F", key+"="+strconv.Itoa(v))
		case []string:
			for _, id := range v {
				args = append(args, "-f", key+"[]="+id)
			}
		default:
			return nil, errors.New("unsupported GitHub variable")
		}
	}
	b, err := exec.CommandContext(ctx, "gh", args...).Output()
	if err != nil {
		return nil, errors.New("GitHub Project read failed")
	}
	return b, nil
}

type fileReader string

func (path fileReader) ProjectItems(ctx context.Context) ([]domain.ProjectItem, error) {
	var items []domain.ProjectItem
	err := pybridge.ReadJSON(string(path), &items)
	if err == nil && items == nil {
		err = errors.New("expected normalized Project item array")
	}
	return items, err
}

func run() error {
	snapshot := flag.String("snapshot", "", "existing Python broker snapshot (read-only)")
	items := flag.String("github-items", "", "offline normalized Project item array; omit for live gh reads")
	repo := flag.String("repo", "", "repository owner/name for live reads")
	owner := flag.String("owner", "", "GitHub Project owner for live reads")
	project := flag.Int("project", 0, "GitHub Project number for live reads")
	flag.Parse()
	if *snapshot == "" {
		return errors.New("-snapshot is required")
	}
	var reader ghgateway.Reader = &ghgateway.Gateway{Source: ghSource{}, Repo: *repo, Owner: *owner, Number: *project}
	if *items != "" {
		reader = fileReader(*items)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	events, err := pybridge.Observe(ctx, *snapshot, reader, 0)
	if err != nil {
		return err
	}
	// Always create private, separate output: an input path can never be selected
	// as the journal destination. The one-shot journal remains for inspection.
	dir, err := os.MkdirTemp("", "pybridge-journal-")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "journal.json")
	j, err := journal.Open(path, journal.Limits{Retain: max(1, len(events)), Pending: 1, MaxEventBytes: 1 << 20})
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := j.Append(event); err != nil {
			return err
		}
	}
	log.Printf("temporary migration observer journal: %s", path)
	return json.NewEncoder(os.Stdout).Encode(j.Snapshot())
}
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
