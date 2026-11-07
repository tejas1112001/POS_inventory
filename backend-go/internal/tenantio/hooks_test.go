package tenantio

import (
	"context"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// SetAfterLoad runs fn after the load and before the comparison (tests:
// prove the comparison catches a difference).
func SetAfterLoad(r *Restorer, fn func(context.Context, tenancy.ScratchSchema) error) {
	r.afterLoad = fn
}
