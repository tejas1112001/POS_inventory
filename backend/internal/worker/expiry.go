package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/tejas1112001/POS_inventory/backend/internal/mailtmpl"
	"github.com/tejas1112001/POS_inventory/backend/internal/outbox"
	"github.com/tejas1112001/POS_inventory/backend/internal/reports"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// The owner's daily expiry email (industry P3 item 7, design §7). Off by
// default (settings.expiry_digest). Like the low-stock email, the hourly
// job queues it from 08:00 shop time, once a day (the outbox key carries
// the day), and only on days with expired, expiring, quarantined or
// recalled stock. Every active owner gets it.

const (
	qExpirySettings = `SELECT timezone, expiry_digest AND 'batch_expiry' = ANY(modules), near_expiry_days FROM settings WHERE id = 1`
	qExpirySentDay  = `SELECT EXISTS (SELECT 1 FROM email_outbox WHERE kind = 'expiry_digest' AND idempotency_key LIKE $1)`
)

type expiryData struct {
	Date        string                `json:"date"`
	WindowDays  int                   `json:"windowDays"`
	Expired     int                   `json:"expired"`
	Expiring    int                   `json:"expiring"`
	Quarantined int                   `json:"quarantined"`
	Recalled    int                   `json:"recalled"`
	Count       int                   `json:"count"`
	Items       []mailtmpl.ExpiryItem `json:"items"`
}

// ExpiryDigests queues the day's expiry email for every shop that is due.
// It returns how many emails were queued.
func (m *Maintenance) ExpiryDigests(ctx context.Context, now time.Time) (int, error) {
	queued, err := m.eachTenant(ctx, "expiry_digest", func(ctx context.Context, t tenancy.Tenant) (int64, error) {
		var n int
		err := m.Jobs.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
			var err error
			n, err = expiryDigestFor(ctx, tx, now)
			return err
		})
		return int64(n), err
	})
	return int(queued), err
}

func expiryDigestFor(ctx context.Context, tx tenancy.TenantTx, now time.Time) (int, error) {
	var tz string
	var on bool
	var window int
	if err := tx.QueryRow(ctx, qExpirySettings).Scan(&tz, &on, &window); err != nil || !on {
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
	if err := tx.QueryRow(ctx, qExpirySentDay, "expiry_digest:"+day+":%").Scan(&sent); err != nil || sent {
		return 0, err
	}
	rep, err := reports.ExpiryRows(ctx, tx, 0, window, "", reports.MaxXLSXRows)
	if err != nil {
		return 0, err
	}
	d := expiryData{Date: local.Format("02 Jan 2006"), WindowDays: window, Count: len(rep.Items),
		Expired: rep.Summary["expired"].Units, Expiring: rep.Summary["expiring"].Units,
		Quarantined: rep.Summary["quarantined"].Units, Recalled: rep.Summary["recalled"].Units, Items: []mailtmpl.ExpiryItem{}}
	if d.Count == 0 {
		return 0, nil
	}
	for i, x := range rep.Items {
		if i >= 50 {
			break
		}
		batch, exp := x.BatchNo, ""
		if x.Unknown {
			batch = "No batch"
		}
		if x.Expiry != nil {
			exp = *x.Expiry
		}
		d.Items = append(d.Items, mailtmpl.ExpiryItem{Section: x.Section, Name: x.ProductName, Batch: batch, Expiry: exp,
			Store: x.StoreName, Qty: strconv.Itoa(x.Quantity)})
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
		ok, err := outbox.Enqueue(ctx, tx, outbox.Item{Key: fmt.Sprintf("expiry_digest:%s:%d", day, o.id), Kind: outbox.KindExpiryDigest,
			To: o.email, ToName: o.name, Template: mailtmpl.ExpiryDigestTemplate, Data: data})
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

// buildExpiryDigest renders a queued expiry email.
func (m *Mailer) buildExpiryDigest(st *tenantState, r row, c *content) error {
	var d expiryData
	if err := json.Unmarshal(r.data, &d); err != nil || d.Count == 0 {
		return fmt.Errorf("%w: bad expiry data", errCancel)
	}
	var err error
	c.rendered, err = mailtmpl.RenderExpiryDigest(mailtmpl.ExpiryDigest{BusinessName: st.biz.Name, Name: c.to.Name, Date: d.Date,
		Link: m.tenantURL(st.t) + "/reports/expiry", Expired: d.Expired, Expiring: d.Expiring, Quarantined: d.Quarantined,
		Recalled: d.Recalled, WindowDays: d.WindowDays, Count: d.Count, Items: d.Items})
	return err
}
