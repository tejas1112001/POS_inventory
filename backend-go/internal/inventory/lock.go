package inventory

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Key is one (store, variant) inventory row.
type Key struct{ Store, Variant int64 }

// Shortage is one entry of the INSUFFICIENT_STOCK details.
type Shortage struct {
	VariantID int64  `json:"variantId"`
	SKU       string `json:"sku"`
	Available int    `json:"available"`
	Requested int    `json:"requested"`
}

// Lock locks every inventory row with a non-zero delta in (store, variant)
// order and checks that no decrement goes below zero. Deterministic lock
// order keeps concurrent documents deadlock-free (PHASE1_DESIGN §4.4). It
// returns the keys to move, in lock order, and the quantities before the
// change (missing rows count as 0). skus names variants in the error.
func Lock(ctx context.Context, tx tenancy.TenantTx, deltas map[Key]int, skus map[int64]string) ([]Key, map[Key]int, error) {
	keys := make([]Key, 0, len(deltas))
	for k, d := range deltas {
		if d != 0 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].Store != keys[b].Store {
			return keys[a].Store < keys[b].Store
		}
		return keys[a].Variant < keys[b].Variant
	})
	have := map[Key]int{}
	for i := 0; i < len(keys); {
		j := i
		var vids []int64
		for ; j < len(keys) && keys[j].Store == keys[i].Store; j++ {
			vids = append(vids, keys[j].Variant)
		}
		q, err := LockRows(ctx, tx, keys[i].Store, vids)
		if err != nil {
			return nil, nil, err
		}
		for v, n := range q {
			have[Key{keys[i].Store, v}] = n
		}
		i = j
	}
	var short []Shortage
	for _, k := range keys {
		if d := deltas[k]; d < 0 && have[k]+d < 0 {
			short = append(short, Shortage{VariantID: k.Variant, SKU: skus[k.Variant], Available: have[k], Requested: -d})
		}
	}
	if len(short) > 0 {
		msg := fmt.Sprintf("Insufficient stock for %s. Available: %d, requested: %d.", short[0].SKU, short[0].Available, short[0].Requested)
		return nil, nil, apperr.Conflict(apperr.CodeInsufficientStock, msg).WithDetails(map[string]any{"items": short})
	}
	return keys, have, nil
}

// Apply moves locked rows by their deltas and writes one ledger row each.
func Apply(ctx context.Context, tx tenancy.TenantTx, keys []Key, deltas map[Key]int, reason string, ref Ref, userID int64) error {
	for _, k := range keys {
		if _, err := Move(ctx, tx, k.Store, k.Variant, deltas[k], reason, ref, userID); err != nil {
			if errors.Is(err, ErrInsufficient) {
				return apperr.Conflict(apperr.CodeInsufficientStock, "Insufficient stock.")
			}
			return err
		}
	}
	return nil
}

// LockAndApply is Lock followed by Apply.
func LockAndApply(ctx context.Context, tx tenancy.TenantTx, deltas map[Key]int, skus map[int64]string, reason string, ref Ref, userID int64) error {
	keys, _, err := Lock(ctx, tx, deltas, skus)
	if err != nil {
		return err
	}
	return Apply(ctx, tx, keys, deltas, reason, ref, userID)
}
