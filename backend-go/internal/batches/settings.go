package batches

import (
	"fmt"
	"net/http"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Batch settings (industry P3 item 7, A34, DB36). Like label layouts they
// are settings: kept, readable and editable while the module is off.

const (
	maxNearExpiryDays = 730
	maxMinDaysToSell  = 365
)

// SettingsJSON is GET/PUT /api/settings/batches.
type SettingsJSON struct {
	NearExpiryDays int  `json:"nearExpiryDays"`
	MinDaysToSell  int  `json:"minDaysToSell"`
	ExpiryDigest   bool `json:"expiryDigest"`
}

type settingsIn struct {
	NearExpiryDays *int  `json:"nearExpiryDays"`
	MinDaysToSell  *int  `json:"minDaysToSell"`
	ExpiryDigest   *bool `json:"expiryDigest"`
}

const (
	qGetBatchSettings     = `SELECT near_expiry_days, min_days_to_sell, expiry_digest FROM settings WHERE id = 1`
	qGetBatchSettingsLock = qGetBatchSettings + ` FOR UPDATE`
	qSetBatchSettings     = `UPDATE settings SET near_expiry_days = $1, min_days_to_sell = $2, expiry_digest = $3, updated_at = now() WHERE id = 1`
)

func (a *API) getSettings(w http.ResponseWriter, r *http.Request) {
	var out SettingsJSON
	err := a.ro(r, func(tx tenancy.TenantTx) error {
		return tx.QueryRow(r.Context(), qGetBatchSettings).Scan(&out.NearExpiryDays, &out.MinDaysToSell, &out.ExpiryDigest)
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) putSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := map[string]string{}
	if in.NearExpiryDays != nil && (*in.NearExpiryDays < 1 || *in.NearExpiryDays > maxNearExpiryDays) {
		f["nearExpiryDays"] = fmt.Sprintf("Enter 1 to %d days.", maxNearExpiryDays)
	}
	if in.MinDaysToSell != nil && (*in.MinDaysToSell < 0 || *in.MinDaysToSell > maxMinDaysToSell) {
		f["minDaysToSell"] = fmt.Sprintf("Enter 0 to %d days.", maxMinDaysToSell)
	}
	if len(f) > 0 {
		httpx.WriteError(w, r, apperr.Fields(f))
		return
	}
	var out SettingsJSON
	err := a.rw(r, func(tx tenancy.TenantTx) error {
		var before SettingsJSON
		if err := tx.QueryRow(r.Context(), qGetBatchSettingsLock).Scan(&before.NearExpiryDays, &before.MinDaysToSell, &before.ExpiryDigest); err != nil {
			return err
		}
		out = before
		if in.NearExpiryDays != nil {
			out.NearExpiryDays = *in.NearExpiryDays
		}
		if in.MinDaysToSell != nil {
			out.MinDaysToSell = *in.MinDaysToSell
		}
		if in.ExpiryDigest != nil {
			out.ExpiryDigest = *in.ExpiryDigest
		}
		if out == before {
			return nil
		}
		if _, err := tx.Exec(r.Context(), qSetBatchSettings, out.NearExpiryDays, out.MinDaysToSell, out.ExpiryDigest); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{ActorID: principal(r).UserID, Action: "settings.batches",
			Entity: "settings", EntityID: "batches", Before: map[string]any{"nearExpiryDays": before.NearExpiryDays,
				"minDaysToSell": before.MinDaysToSell, "expiryDigest": before.ExpiryDigest},
			After: map[string]any{"nearExpiryDays": out.NearExpiryDays, "minDaysToSell": out.MinDaysToSell, "expiryDigest": out.ExpiryDigest}})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
