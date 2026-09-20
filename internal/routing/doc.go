// Package routing selects policy-valid execution tuples from an eligible frontier
// and interprets legacy routing records. It grants no dispatch authority.
package routing

import (
	"context"

	"github.com/PsyChaos/ai-team-harness/internal/domain"
)

// TaskRouter returns routing evidence for a task; it grants no dispatch authority.
type TaskRouter interface {
	RouteTask(context.Context, domain.ProjectItem) (domain.RoutingResult, error)
}
