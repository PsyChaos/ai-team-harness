// Package routing defines the boundary for interpreting existing routing records.
// Provider selection and model invocation are not implemented in this foundation.
package routing

import (
	"context"

	"github.com/PsyChaos/ai-team-harness/internal/domain"
)

// TaskRouter returns routing evidence for a task; it grants no dispatch authority.
type TaskRouter interface {
	RouteTask(context.Context, domain.ProjectItem) (domain.RoutingResult, error)
}
