// Package ghgateway defines the read boundary for normalized GitHub state.
// Gateway reconstructs recorded or supplied API responses without mutations.
// Transport and authentication belong to the caller-provided Source.
package ghgateway

import (
	"context"

	"github.com/PsyChaos/ai-team-harness/internal/domain"
)

// Reader retrieves complete normalized Project items or reports an error.
// Implementations must retain dependency completeness in each item.
type Reader interface {
	ProjectItems(context.Context) ([]domain.ProjectItem, error)
}
