package sales

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Held bills (S10 item 7, docs/S10_DESIGN.md §7, D9). Every cashier of a
// store (pos:create) sees that store's held carts and can recall one;
// recalling removes it, so two tills cannot both take it. Nothing is
// reserved: the checkout re-checks prices and stock when it loads the cart.

// MaxHeldPerUser is how many carts one user may hold at a time.
const MaxHeldPerUser = 20

// HeldLine is one line of a held cart (display fields kept for the list).
type HeldLine struct {
	ProductID int64            `json:"productId"`
	VariantID int64            `json:"variantId"`
	Quantity  int64            `json:"quantity"`
	UnitPrice *decimal.Decimal `json:"unitPrice"`
	Name      string           `json:"name"`
	Label     string           `json:"label"`
}

// HeldCart is a parked cart.
type HeldCart struct {
	ID           int64           `json:"id"`
	StoreID      int64           `json:"storeId"`
	UserID       int64           `json:"userId"`
	UserName     *string         `json:"userName"`
	CustomerID   *int64          `json:"customerId"`
	CustomerName *string         `json:"customerName"`
	Label        *string         `json:"label"`
	Lines        []HeldLine      `json:"lines"`
	Discount     decimal.Decimal `json:"discount"`
	Items        int64           `json:"items"`    // units in the cart
	Estimate     decimal.Decimal `json:"estimate"` // at the held prices, before tax: the checkout re-prices
	CreatedAt    time.Time       `json:"createdAt"`
	ExpiresAt    time.Time       `json:"expiresAt"`
}

const qHeldCols = `h.id, h.store_id, h.user_id, u.name, h.customer_id, c.name, h.label, h.lines, h.discount, h.created_at, h.expires_at
	FROM held_carts h LEFT JOIN users u ON u.id = h.user_id LEFT JOIN customers c ON c.id = h.customer_id`

// qTakeHeld removes a held cart and returns it with its names.
const qTakeHeld = `WITH h AS (DELETE FROM held_carts WHERE id = $1 AND expires_at > now() RETURNING *)
	SELECT h.id, h.store_id, h.user_id, u.name, h.customer_id, c.name, h.label, h.lines, h.discount, h.created_at, h.expires_at
	FROM h LEFT JOIN users u ON u.id = h.user_id LEFT JOIN customers c ON c.id = h.customer_id`

func scanHeld(row interface{ Scan(...any) error }) (HeldCart, error) {
	var h HeldCart
	var raw []byte
	if err := row.Scan(&h.ID, &h.StoreID, &h.UserID, &h.UserName, &h.CustomerID, &h.CustomerName, &h.Label, &raw, &h.Discount,
		&h.CreatedAt, &h.ExpiresAt); err != nil {
		return h, err
	}
	if err := json.Unmarshal(raw, &h.Lines); err != nil {
		return h, err
	}
	for _, l := range h.Lines {
		h.Items += l.Quantity
		if l.UnitPrice != nil {
			h.Estimate = h.Estimate.Add(l.UnitPrice.Mul(decimal.NewFromInt(l.Quantity)))
		}
	}
	h.Estimate = decimal.Max(h.Estimate.Sub(h.Discount), decimal.Zero)
	return h, nil
}

func (a *API) listHeld(w http.ResponseWriter, r *http.Request) {
	store := queryInt(r, "storeId")
	if store <= 0 {
		httpx.WriteError(w, r, apperr.Invalid("storeId", "storeId is required."))
		return
	}
	out := []HeldCart{}
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		rows, err := tx.Query(r.Context(), `SELECT `+qHeldCols+` WHERE h.store_id = $1 AND h.expires_at > now()
			ORDER BY h.created_at DESC LIMIT 200`, store)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			h, err := scanHeld(rows)
			if err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, out)
}

type heldLineIn struct {
	ProductID money.Int `json:"productId"`
	VariantID money.Int `json:"variantId"`
	Quantity  money.Int `json:"quantity"`
	UnitPrice money.Dec `json:"unitPrice"`
	Name      string    `json:"name"`
	Label     string    `json:"label"`
}

type holdIn struct {
	StoreID    money.Int    `json:"storeId"`
	CustomerID money.Int    `json:"customerId"`
	Label      string       `json:"label"`
	Discount   money.Dec    `json:"discount"`
	Lines      []heldLineIn `json:"lines"`
}

func clip(s string, n int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s))
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// hold is POST /api/held-carts.
func (a *API) hold(w http.ResponseWriter, r *http.Request) {
	var in holdIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := fieldErrs{}
	if !in.StoreID.Valid || in.StoreID.V <= 0 {
		f["storeId"] = "storeId is required."
	}
	switch {
	case len(in.Lines) == 0:
		f["lines"] = "The cart is empty."
	case len(in.Lines) > maxLines:
		f["lines"] = fmt.Sprintf("At most %d lines.", maxLines)
	}
	lines := make([]HeldLine, 0, len(in.Lines))
	for i, l := range in.Lines {
		if !l.VariantID.Valid || l.VariantID.V <= 0 || !l.ProductID.Valid || l.ProductID.V <= 0 {
			f[fmt.Sprintf("lines[%d].variantId", i)] = "productId and variantId are required."
		}
		if !l.Quantity.Valid || l.Quantity.V <= 0 || l.Quantity.V > maxQty {
			f[fmt.Sprintf("lines[%d].quantity", i)] = "Quantity must be a whole number above 0."
		}
		h := HeldLine{ProductID: l.ProductID.V, VariantID: l.VariantID.V, Quantity: l.Quantity.V, Name: clip(l.Name, 160), Label: clip(l.Label, 120)}
		if l.UnitPrice.Valid {
			if l.UnitPrice.D.IsNegative() {
				f[fmt.Sprintf("lines[%d].unitPrice", i)] = "Must not be negative."
			}
			p := l.UnitPrice.D.Round(4)
			h.UnitPrice = &p
		}
		lines = append(lines, h)
	}
	discount := decimal.Zero
	if in.Discount.Valid {
		if in.Discount.D.IsNegative() {
			f["discount"] = "Must not be negative."
		}
		discount = in.Discount.D.Round(4)
	}
	label := clip(in.Label, 60)
	if err := f.err(); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	raw, _ := json.Marshal(lines)
	p := principal(r)
	var out HeldCart
	err := a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		var storeOK, custOK bool
		var mine int
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stores WHERE id = $1 AND deleted_at IS NULL AND status = 'Active'),
				$2 = 0 OR EXISTS (SELECT 1 FROM customers WHERE id = $2 AND deleted_at IS NULL),
				(SELECT count(*) FROM held_carts WHERE user_id = $3 AND expires_at > now())`, in.StoreID.V, in.CustomerID.V, p.UserID).
			Scan(&storeOK, &custOK, &mine); err != nil {
			return err
		}
		if !storeOK {
			return apperr.Invalid("storeId", "Store not found or inactive.")
		}
		if !custOK {
			return apperr.Invalid("customerId", "Customer not found.")
		}
		if mine >= MaxHeldPerUser {
			return apperr.Conflict(apperr.CodeInvalidState, fmt.Sprintf("You already hold %d bills. Recall or discard one first.", MaxHeldPerUser))
		}
		var customer *int64
		if in.CustomerID.Valid && in.CustomerID.V > 0 {
			customer = &in.CustomerID.V
		}
		var id int64
		if err := tx.QueryRow(ctx, `INSERT INTO held_carts (store_id, user_id, customer_id, label, lines, discount)
			VALUES ($1, $2, $3, NULLIF($4, ''), $5::text::jsonb, $6) RETURNING id`, in.StoreID.V, p.UserID, customer, label, string(raw), discount).Scan(&id); err != nil {
			return err
		}
		var err error
		if out, err = scanHeld(tx.QueryRow(ctx, `SELECT `+qHeldCols+` WHERE h.id = $1`, id)); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "held_cart.hold", Entity: "held_cart", EntityID: strconv.FormatInt(id, 10),
			After: map[string]any{"store_id": in.StoreID.V, "lines": len(lines), "label": label}, RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

// take removes a held cart and returns it (recall), or just removes it
// (discard). The DELETE … RETURNING makes it once only.
func (a *API) take(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathID(r)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		p := principal(r)
		var out HeldCart
		err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
			ctx := r.Context()
			var err error
			out, err = scanHeld(tx.QueryRow(ctx, qTakeHeld, id))
			if isNoRows(err) {
				return apperr.NotFound()
			}
			if err != nil {
				return err
			}
			return audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "held_cart." + action, Entity: "held_cart",
				EntityID: strconv.FormatInt(id, 10), Before: map[string]any{"held_by": out.UserID, "store_id": out.StoreID, "lines": len(out.Lines)},
				RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()})
		})
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if action == "discard" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	}
}
