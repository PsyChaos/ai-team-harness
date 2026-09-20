// Package ghgateway defines the read boundary for normalized GitHub state.
// No authentication, network implementation, or mutation is provided yet.
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
