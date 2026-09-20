package ghgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/PsyChaos/ai-team-harness/internal/domain"
)

// Source supplies read-only GitHub responses. Implementations must report API and
// transport failures; GraphQL responses containing errors are also rejected here.
// Variables are GitHub GraphQL variables, with cursors omitted on the first page.
// No credentials or network implementation are supplied by this package.
type Source interface {
	OwnerType(context.Context, string) (string, error)
	GraphQL(context.Context, string, map[string]any) (json.RawMessage, error)
}

// Gateway reconstructs the Python broker's normalized Project view. Repo is an
// owner/name pair; Owner and Number identify the Project (not the repository).
type Gateway struct {
	Source Source
	Repo   string
	Owner  string
	Number int
}

var _ Reader = (*Gateway)(nil)

type connection struct {
	Nodes    []json.RawMessage `json:"nodes"`
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
}

func (g *Gateway) query(ctx context.Context, query string, variables map[string]any, target any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := g.Source.GraphQL(ctx, query, variables)
	if err != nil {
		return err
	}
	var envelope struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	if len(envelope.Errors) != 0 || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return fmt.Errorf("GitHub GraphQL response is incomplete")
	}
	return json.Unmarshal(envelope.Data, target)
}

// ProjectItems reads at most twenty pages and hydrates BLOCKED dependencies in
// batches of one hundred, matching broker.project_items. Other issues retain
// complete=false, even if they have no recorded dependencies. Partial results
// are never returned on error; truncated blocker lists remain explicitly incomplete.
func (g *Gateway) ProjectItems(ctx context.Context) ([]domain.ProjectItem, error) {
	if g.Source == nil || g.Repo == "" || g.Owner == "" || g.Number < 1 {
		return nil, fmt.Errorf("invalid Project configuration")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ownerType, err := g.Source.OwnerType(ctx, g.Owner)
	if err != nil {
		return nil, err
	}
	root := "user"
	if ownerType == "Organization" {
		root = "organization"
	} else if ownerType != "User" {
		return nil, fmt.Errorf("invalid Project owner type")
	}
	items := []domain.ProjectItem{}
	cursor := ""
	for page := 0; page < 20; page++ {
		variables := map[string]any{"login": g.Owner, "number": g.Number}
		if cursor != "" {
			variables["cursor"] = cursor
		}
		var data map[string]*struct {
			Project *struct {
				Items *connection `json:"items"`
			} `json:"projectV2"`
		}
		if err := g.query(ctx, projectQuery(root), variables, &data); err != nil {
			return nil, err
		}
		value := data[root]
		if value == nil || value.Project == nil || value.Project.Items == nil || value.Project.Items.Nodes == nil {
			return nil, fmt.Errorf("invalid Project item response")
		}
		conn := value.Project.Items
		for _, raw := range conn.Nodes {
			item, err := normalizeItem(raw, g.Repo)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		if !conn.PageInfo.HasNextPage {
			if err := g.hydrate(ctx, items); err != nil {
				return nil, err
			}
			return items, nil
		}
		cursor = conn.PageInfo.EndCursor
		if cursor == "" {
			return nil, fmt.Errorf("invalid Project pagination")
		}
	}
	return nil, fmt.Errorf("Project exceeds bounded page limit")
}

func normalizeItem(raw json.RawMessage, repo string) (domain.ProjectItem, error) {
	var wire struct {
		ID       *string `json:"id"`
		Archived *bool   `json:"isArchived"`
		Content  struct {
			domain.Issue
			Typename string `json:"__typename"`
		} `json:"content"`
		Fields *connection `json:"fieldValues"`
	}
	var item domain.ProjectItem
	if err := json.Unmarshal(raw, &wire); err != nil {
		return item, err
	}
	if wire.ID == nil || wire.Archived == nil || *wire.Archived {
		return item, fmt.Errorf("invalid or archived Project item")
	}
	if wire.Fields == nil || wire.Fields.Nodes == nil || wire.Fields.PageInfo.HasNextPage {
		return item, fmt.Errorf("Project field values are incomplete")
	}
	item.ID = *wire.ID
	item.FieldValues.Nodes = wire.Fields.Nodes
	item.Content.Type = wire.Content.Typename
	if wire.Content.Typename != "Issue" {
		return item, nil
	}
	if wire.Content.Repository.NameWithOwner != repo {
		return item, fmt.Errorf("Project issue identity is invalid")
	}
	item.Content = wire.Content.Issue
	item.Content.Type = "Issue"
	item.Content.BlockedBy = domain.Blockers{Nodes: []domain.Dependency{}, Complete: false}
	return item, nil
}

func (g *Gateway) hydrate(ctx context.Context, items []domain.ProjectItem) error {
	indices := []int{}
	for i, item := range items {
		if FieldValue(item, "Harness Status") == "BLOCKED" && item.Content.Type == "Issue" {
			indices = append(indices, i)
		}
	}
	for start := 0; start < len(indices); start += 100 {
		end := min(start+100, len(indices))
		ids := []string{}
		for _, i := range indices[start:end] {
			ids = append(ids, items[i].Content.ID)
		}
		var response struct {
			Nodes []json.RawMessage `json:"nodes"`
		}
		if err := g.query(ctx, blockersQuery, map[string]any{"ids": ids}, &response); err != nil {
			return err
		}
		if response.Nodes == nil || len(response.Nodes) != len(ids) {
			return fmt.Errorf("invalid batched dependency response")
		}
		for j, raw := range response.Nodes {
			blockers, err := normalizeBlockers(raw, ids[j])
			if err != nil {
				return err
			}
			items[indices[start+j]].Content.BlockedBy = blockers
		}
	}
	return nil
}

func normalizeBlockers(raw json.RawMessage, id string) (domain.Blockers, error) {
	result := domain.Blockers{Nodes: []domain.Dependency{}}
	var value struct {
		ID        string `json:"id"`
		BlockedBy *struct {
			Nodes []json.RawMessage `json:"nodes"`
			Page  *struct {
				HasNextPage bool `json:"hasNextPage"`
			} `json:"pageInfo"`
		} `json:"blockedBy"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return result, err
	}
	if value.ID != id || value.BlockedBy == nil || value.BlockedBy.Nodes == nil || value.BlockedBy.Page == nil {
		return result, fmt.Errorf("invalid dependency identity or connection")
	}
	for _, node := range value.BlockedBy.Nodes {
		var dep struct {
			Number     *int               `json:"number"`
			URL        *string            `json:"url"`
			Title      *string            `json:"title"`
			State      string             `json:"state"`
			Repository *domain.Repository `json:"repository"`
		}
		if err := json.Unmarshal(node, &dep); err != nil {
			return result, err
		}
		if dep.Number == nil || dep.URL == nil || dep.Title == nil || dep.Repository == nil || (dep.State != "OPEN" && dep.State != "CLOSED") {
			return result, fmt.Errorf("invalid dependency")
		}
		result.Nodes = append(result.Nodes, domain.Dependency{Number: *dep.Number, URL: *dep.URL, Title: *dep.Title, State: dep.State, Repository: dep.Repository})
	}
	result.Complete = !value.BlockedBy.Page.HasNextPage
	return result, nil
}

// FieldValue reads an exact named field from normalized Project nodes, preserving
// Python's number-before-text precedence and integral numeric formatting. Missing
// fields yield an empty string. Flattened gh CLI items are not normalized input.
func FieldValue(item domain.ProjectItem, name string) string {
	for _, raw := range item.FieldValues.Nodes {
		var node struct {
			Field struct {
				Name string `json:"name"`
			} `json:"field"`
			Number *float64 `json:"number"`
			Name   string   `json:"name"`
			Text   string   `json:"text"`
			Title  string   `json:"title"`
		}
		if json.Unmarshal(raw, &node) != nil || node.Field.Name != name {
			continue
		}
		if node.Number != nil {
			return strconv.FormatFloat(*node.Number, 'f', -1, 64)
		}
		for _, value := range []string{node.Name, node.Text, node.Title} {
			if value != "" {
				return value
			}
		}
		return ""
	}
	return ""
}

// TaskState is a read-only projection of a normalized item. RetryCount retains
// the broker field string (including an empty value); it does not grant retries.
// Labels and Lease are null because the current broker does not read either.
// In particular CLAIMED/IN_PROGRESS must not be interpreted as a live lease.
type TaskState struct {
	Item       domain.ProjectItem `json:"item"`
	Status     string             `json:"status"`
	RetryCount string             `json:"retry_count"`
	Provider   string             `json:"provider"`
	Role       string             `json:"role"`
	Labels     []string           `json:"labels"`
	Lease      json.RawMessage    `json:"lease"`
}

// Reconstruct projects durable fields without inferring worker liveness or
// authorizing dispatch. Unknown labels/leases remain distinct from empty state.
func Reconstruct(items []domain.ProjectItem) []TaskState {
	states := make([]TaskState, 0, len(items))
	for _, item := range items {
		states = append(states, TaskState{Item: item, Status: FieldValue(item, "Harness Status"), RetryCount: FieldValue(item, "Retry Count"), Provider: FieldValue(item, "Provider"), Role: FieldValue(item, "Agent Role")})
	}
	return states
}
