package products

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/modules"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// BatchSummary is a tracked product's batch stock at a glance (industry P3
// item 1, A25): sellable units (available, not expired; the unknown batch
// included, sold with a warning), units in the unknown batch, in
// quarantine, in expired batches, and the nearest expiry still to come
// among batches with stock.
type BatchSummary struct {
	Sellable      int     `json:"sellable"`
	Unbatched     int     `json:"unbatched"`
	Quarantined   int     `json:"quarantined"`
	Expired       int     `json:"expired"`
	NearestExpiry *string `json:"nearestExpiry"`
}

// setTracking applies a product's "tracks batches" choice (industry P3
// item 1, A25, design §1.3). want nil keeps the product as it is, except
// that a new product in a shop with the module on tracks batches by default
// (§0). Choosing anything other than that default needs batches:manage;
// sending the field while the module is off is refused (403 MODULE_OFF).
func setTracking(ctx context.Context, tx tenancy.TenantTx, p auth.Principal, productID int64, want *bool, created bool, requestID, ua string) error {
	on, err := inventory.ModuleOn(ctx, tx)
	if err != nil {
		return err
	}
	if !on {
		if want != nil {
			return modules.ErrOff(modules.BatchExpiry)
		}
		return nil
	}
	var cur bool
	err = tx.QueryRow(ctx, `SELECT tracks_batches FROM products WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, productID).Scan(&cur)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound()
	}
	if err != nil {
		return err
	}
	def := cur
	if created {
		def = true
	}
	v := def
	if want != nil {
		v = *want
	}
	if v != def && !p.Has("batches:manage") {
		return apperr.Forbidden(apperr.CodeForbidden, "Changing whether a product tracks batches needs the manage batches permission.")
	}
	if v == cur {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE products SET tracks_batches = $2, updated_at = now() WHERE id = $1`, productID, v); err != nil {
		return err
	}
	if v {
		err = inventory.StartTracking(ctx, tx, []int64{productID}, p.UserID)
	} else {
		err = inventory.StopTracking(ctx, tx, []int64{productID}, p.UserID)
	}
	if err != nil {
		return err
	}
	return audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "product.batch_tracking", Entity: "product",
		EntityID: strconv.FormatInt(productID, 10), Before: map[string]any{"tracksBatches": cur},
		After: map[string]any{"tracksBatches": v}, RequestID: requestID, UserAgent: ua})
}

// trackingFor is setTracking with the request's details.
func trackingFor(ctx context.Context, tx tenancy.TenantTx, p auth.Principal, productID int64, want *bool, created bool, ua string) error {
	return setTracking(ctx, tx, p, productID, want, created, httpx.RequestID(ctx), ua)
}
