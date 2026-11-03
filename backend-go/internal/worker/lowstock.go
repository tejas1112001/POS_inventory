package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailtmpl"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/outbox"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// The owner's daily low-stock email (S10 item 6, D8). The hourly job
// queues it from 08:00 shop time, once a day (the outbox key carries the
// day), only when items are at or below their alert level and the setting
// is on. Every active owner gets it.

// LowStockHour is the shop-time hour from which the email goes out.
const LowStockHour = 8

const (
	qLowStockSettings = `SELECT timezone, low_stock_email FROM settings WHERE id = 1`
	qLowStockSentDay  = `SELECT EXISTS (SELECT 1 FROM email_outbox WHERE kind = 'low_stock' AND idempotency_key LIKE $1)`
	qLowStockItems    = `SELECT p.name, coalesce(v.sku::text, ''), st.name, i.qty, i.quantity_alert, count(*) OVER ()
		FROM inventories i
		JOIN product_variants v ON v.id = i.variant_id AND v.deleted_at IS NULL
		JOIN products p ON p.id = v.product_id AND p.deleted_at IS NULL
		JOIN stores st ON st.id = i.store_id AND st.deleted_at IS NULL AND st.status = 'Active'
		WHERE i.qty <= i.quantity_alert
		ORDER BY i.qty, p.name, st.name LIMIT 50`
	qOwners = `SELECT u.id, u.email::text, u.name FROM users u JOIN roles r ON r.id = u.role_id
		WHERE r.system_key = 'owner' AND u.status = 'Active' AND u.deleted_at IS NULL AND u.email IS NOT NULL`
)

type lowStockItem struct {
	Name  string `json:"name"`
	SKU   string `json:"sku"`
	Store string `json:"store"`
	Qty   string `json:"qty"`
	Alert string `json:"alert"`
}

type lowStockData struct {
	Date  string         `json:"date"`
	Count int            `json:"count"`
	Items []lowStockItem `json:"items"`
}

// LowStockEmails queues the day's email for every shop that is due. It
// returns how many emails were queued.
func (m *Maintenance) LowStockEmails(ctx context.Context, now time.Time) (int, error) {
	queued := 0
	err := m.eachTenant(ctx, "low_stock_email", func(t tenancy.Tenant) error {
		return m.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
			n, err := lowStockFor(ctx, tx, now)
			queued += n
			return err
		})
	})
	return queued, err
}

func lowStockFor(ctx context.Context, tx tenancy.TenantTx, now time.Time) (int, error) {
	var tz string
	var on bool
	if err := tx.QueryRow(ctx, qLowStockSettings).Scan(&tz, &on); err != nil || !on {
		return 0, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	local := now.In(loc)
	if local.Hour() < LowStockHour {
		return 0, nil
	}
	day := local.Format("2006-01-02")
	var sent bool
	if err := tx.QueryRow(ctx, qLowStockSentDay, "low_stock:"+day+":%").Scan(&sent); err != nil || sent {
		return 0, err
	}
	rows, err := tx.Query(ctx, qLowStockItems)
	if err != nil {
		return 0, err
	}
	d := lowStockData{Date: local.Format("02 Jan 2006"), Items: []lowStockItem{}}
	for rows.Next() {
		var it lowStockItem
		var qty, alert decimal.Decimal
		if err := rows.Scan(&it.Name, &it.SKU, &it.Store, &qty, &alert, &d.Count); err != nil {
			rows.Close()
			return 0, err
		}
		it.Qty, it.Alert = qty.String(), alert.String()
		d.Items = append(d.Items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil || d.Count == 0 {
		return 0, err
	}
	data := map[string]any{}
	b, _ := json.Marshal(d)
	_ = json.Unmarshal(b, &data)
	owners, err := tx.Query(ctx, qOwners)
	if err != nil {
		return 0, err
	}
	type owner struct {
		id          int64
		email, name string
	}
	var list []owner
	for owners.Next() {
		var o owner
		if err := owners.Scan(&o.id, &o.email, &o.name); err != nil {
			owners.Close()
			return 0, err
		}
		list = append(list, o)
	}
	owners.Close()
	if err := owners.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, o := range list {
		ok, err := outbox.Enqueue(ctx, tx, outbox.Item{Key: fmt.Sprintf("low_stock:%s:%d", day, o.id), Kind: outbox.KindLowStock,
			To: o.email, ToName: o.name, Template: mailtmpl.LowStockTemplate, Data: data})
		if errors.Is(err, outbox.ErrInvalid) {
			continue // an owner without a usable address
		}
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

// buildLowStock renders a queued low-stock email.
func (m *Mailer) buildLowStock(st *tenantState, r row, c *content) error {
	var d lowStockData
	if err := json.Unmarshal(r.data, &d); err != nil || d.Count == 0 {
		return fmt.Errorf("%w: bad low-stock data", errCancel)
	}
	items := make([]mailtmpl.LowStockItem, 0, len(d.Items))
	for _, it := range d.Items {
		items = append(items, mailtmpl.LowStockItem{Name: it.Name, SKU: it.SKU, Store: it.Store, Qty: it.Qty, Alert: it.Alert})
	}
	var err error
	c.rendered, err = mailtmpl.RenderLowStock(mailtmpl.LowStock{BusinessName: st.biz.Name, Name: c.to.Name, Date: d.Date,
		Link: m.tenantURL(st.t) + "/inventory", Count: d.Count, Items: items})
	return err
}
