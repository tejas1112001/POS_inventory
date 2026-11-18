package batches

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Settings are the shop's batch settings (DB36) and today's date in its
// time zone.
type Settings struct {
	NearExpiryDays int
	MinDaysToSell  int
	ExpiryDigest   bool
	Today          time.Time
}

// LoadSettings reads the batch settings in the caller's transaction.
func LoadSettings(ctx context.Context, tx tenancy.TenantTx) (Settings, error) {
	var s Settings
	err := tx.QueryRow(ctx, `SELECT near_expiry_days, min_days_to_sell, expiry_digest, `+sqlToday+` FROM settings WHERE id = 1`).
		Scan(&s.NearExpiryDays, &s.MinDaysToSell, &s.ExpiryDigest, &s.Today)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{NearExpiryDays: 90, Today: time.Now().UTC().Truncate(24 * time.Hour)}, nil
	}
	return s, err
}

// Expired reports whether a batch with this expiry is expired today.
func (s Settings) Expired(expiry time.Time) bool { return expiry.Before(s.Today) }

// NearExpiry reports whether a batch expires within the warning window.
func (s Settings) NearExpiry(expiry time.Time) bool {
	return !s.Expired(expiry) && !expiry.After(s.Today.AddDate(0, 0, s.NearExpiryDays))
}

// Arrival is a batch arriving (receiving, counting, import).
type Arrival struct {
	BatchNo string
	Expiry  time.Time
	Mfg     *time.Time
	MRP     *decimal.Decimal
	Cost    *decimal.Decimal
}

// ErrExpiryMismatch means the batch number exists for the product with another
// expiry date, which is almost always a typing error (design §1 DB27).
var ErrExpiryMismatch = errors.New("batch exists with another expiry")

// ErrReservedNo means the batch number is the one kept for stock with no batch.
var ErrReservedNo = errors.New("reserved batch number")

// ValidBatchNo trims a batch number and checks its length.
func ValidBatchNo(s string) (string, bool) {
	s = strings.TrimSpace(s)
	n := len([]rune(s))
	return s, n >= 1 && n <= maxBatchNo
}

// Resolve returns the variant's batch with this number, creating it with
// the arrival's details. An existing batch keeps its details, except that
// MRP and cost fill in when it has none.
func Resolve(ctx context.Context, tx tenancy.TenantTx, variantID int64, a Arrival, userID int64) (int64, bool, error) {
	if strings.EqualFold(strings.TrimSpace(a.BatchNo), inventory.UnknownBatchNo) {
		return 0, false, ErrReservedNo
	}
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO batches (variant_id, batch_no, expiry_date, mfg_date, mrp, cost, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, 0))
		ON CONFLICT (variant_id, batch_no) DO NOTHING RETURNING id`,
		variantID, a.BatchNo, a.Expiry, a.Mfg, a.MRP, a.Cost, userID).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, err
	}
	var exp *time.Time
	if err := tx.QueryRow(ctx, `SELECT id, expiry_date FROM batches WHERE variant_id = $1 AND batch_no = $2 FOR UPDATE`, variantID, a.BatchNo).
		Scan(&id, &exp); err != nil {
		return 0, false, err
	}
	if exp == nil || !exp.Equal(a.Expiry) {
		return 0, false, ErrExpiryMismatch
	}
	if _, err := tx.Exec(ctx, `UPDATE batches SET mrp = coalesce(mrp, $2), cost = coalesce(cost, $3), updated_at = now()
		WHERE id = $1 AND (mrp IS NULL AND $2::numeric IS NOT NULL OR cost IS NULL AND $3::numeric IS NOT NULL)`, id, a.MRP, a.Cost); err != nil {
		return 0, false, err
	}
	return id, false, nil
}
