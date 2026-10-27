package reports

import (
	"context"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/docdate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// DashboardStats are the KPI tiles. totalReturns and netRevenue are new.
type DashboardStats struct {
	TotalRevenue   decimal.Decimal `json:"totalRevenue"`
	TotalOrders    int             `json:"totalOrders"`
	TotalCustomers int             `json:"totalCustomers"`
	TotalProducts  int             `json:"totalProducts"`
	TotalReturns   decimal.Decimal `json:"totalReturns"`
	NetRevenue     decimal.Decimal `json:"netRevenue"`
}

// MonthPoint is one bar of the revenue chart.
type MonthPoint struct {
	Month   string          `json:"month"` // "Oct 2026"
	Revenue decimal.Decimal `json:"revenue"`
	Orders  int             `json:"orders"`
}

// Slice is one slice of the top-products pie chart (category holds the
// product name, as the old API did).
type Slice struct {
	Category string          `json:"category"`
	Value    decimal.Decimal `json:"value"`
	Color    string          `json:"color"`
}

// SummaryCards are the payment tiles.
type SummaryCards struct {
	TotalAmount  decimal.Decimal `json:"totalAmount"`
	TotalPaid    decimal.Decimal `json:"totalPaid"`
	TotalUnpaid  decimal.Decimal `json:"totalUnpaid"`
	TotalOverdue decimal.Decimal `json:"totalOverdue"`
}

// DashboardFilters echoes what the figures cover.
type DashboardFilters struct {
	StartDate *time.Time `json:"startDate"`
	EndDate   *time.Time `json:"endDate"` // exclusive
	StoreID   int64      `json:"storeId"`
	UserID    int64      `json:"userId"` // 0: everyone
	OwnOnly   bool       `json:"ownOnly"`
	Timezone  string     `json:"timezone"`
}

// Dashboard is GET /api/dashboard (same keys as before, C-16).
type Dashboard struct {
	Stats             DashboardStats   `json:"stats"`
	RevenueData       []MonthPoint     `json:"revenueData"`
	SalesDistribution []Slice          `json:"salesDistribution"`
	SummaryCards      SummaryCards     `json:"summaryCards"`
	Filters           DashboardFilters `json:"filters"`
}

var sliceColors = []string{"#4F46E5", "#10B981", "#F59E0B", "#EF4444", "#8B5CF6"}

const chartMonths = 12

// Sales in scope: not void, the user filter, the store and the dates.
const qScope = `s.status <> 'Void' AND ($1 = 0 OR s.user_id = $1) AND ($2 = 0 OR s.store_id = $2)
	AND ($3::timestamptz IS NULL OR s.date >= $3) AND ($4::timestamptz IS NULL OR s.date < $4)`

const (
	qDashTotals = `SELECT coalesce(sum(s.total), 0), count(*), coalesce(sum(s.paid), 0),
			coalesce(sum(s.due) FILTER (WHERE s.payment_status IN ('Unpaid','Partial')), 0),
			coalesce(sum(s.due) FILTER (WHERE s.payment_status = 'Overdue'), 0)
		FROM sales s WHERE ` + qScope
	// Returns dated in the period at the store; for one user, only returns
	// of that user's sales.
	qDashReturns = `SELECT coalesce(sum(r.total), 0) FROM sale_returns r LEFT JOIN sales s ON s.id = r.sale_id
		WHERE ($1 = 0 OR s.user_id = $1) AND ($2 = 0 OR r.store_id = $2)
		AND ($3::timestamptz IS NULL OR r.date >= $3) AND ($4::timestamptz IS NULL OR r.date < $4)`
	qDashCounts = `SELECT (SELECT count(*) FROM customers WHERE deleted_at IS NULL),
		(SELECT count(*) FROM products WHERE deleted_at IS NULL)`
	// Calendar months in the tenant's time zone ($5), zero-filled, from $6
	// to $7 (local month starts).
	qDashMonths = `SELECT to_char(m, 'Mon YYYY'), coalesce(sum(s.total), 0), count(s.id)
		FROM generate_series($6::text::timestamp, $7::text::timestamp, interval '1 month') m
		LEFT JOIN sales s ON date_trunc('month', s.date AT TIME ZONE $5) = m AND ` + qScope + `
		GROUP BY m ORDER BY m`
	qDashTop = `SELECT max(i.product_name), sum(i.subtotal) AS value
		FROM order_items i JOIN sales s ON s.id = i.sale_id
		WHERE ` + qScope + `
		GROUP BY coalesce(i.product_id, -i.id) ORDER BY value DESC, max(i.product_name) LIMIT 5`
)

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// chartWindow is the months to chart, as local month starts (wall clock,
// no zone): the 12 months up to the end date's month (or this month),
// not before the start date's month.
func chartWindow(from, to *time.Time, loc *time.Location, now time.Time) (time.Time, time.Time, bool) {
	lastDay := now.In(loc)
	if to != nil {
		lastDay = to.Add(-time.Microsecond).In(loc)
	}
	last := monthStart(time.Date(lastDay.Year(), lastDay.Month(), 1, 0, 0, 0, 0, time.UTC))
	first := last.AddDate(0, -(chartMonths - 1), 0)
	if from != nil {
		f := from.In(loc)
		if fm := monthStart(time.Date(f.Year(), f.Month(), 1, 0, 0, 0, 0, time.UTC)); fm.After(first) {
			first = fm
		}
	}
	return first, last, !first.After(last)
}

// dashboard is GET /api/dashboard?storeId=&startDate=&endDate=&userId=.
// Without sales:view_all every sales figure is the user's own (userId is
// ignored); with it, userId narrows to one biller.
func (a *API) dashboard(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	scope := ownerScope(p)
	if scope == 0 {
		scope = queryID(r, "userId")
	}
	store := queryID(r, "storeId")
	var out Dashboard
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		b, err := settings.LoadBusiness(ctx, tx)
		if err != nil {
			return err
		}
		loc := location(b)
		q := r.URL.Query()
		from, err := docdate.Bound(q.Get("startDate"), loc, false)
		if err != nil {
			return err
		}
		to, err := docdate.Bound(q.Get("endDate"), loc, true)
		if err != nil {
			return err
		}
		out.Filters = DashboardFilters{StartDate: from, EndDate: to, StoreID: store, UserID: scope, OwnOnly: !p.Has("sales:view_all"),
			Timezone: loc.String()}
		args := []any{scope, store, from, to}
		if err := tx.QueryRow(ctx, qDashTotals, args...).Scan(&out.Stats.TotalRevenue, &out.Stats.TotalOrders,
			&out.SummaryCards.TotalPaid, &out.SummaryCards.TotalUnpaid, &out.SummaryCards.TotalOverdue); err != nil {
			return err
		}
		out.SummaryCards.TotalAmount = out.Stats.TotalRevenue
		if err := tx.QueryRow(ctx, qDashReturns, args...).Scan(&out.Stats.TotalReturns); err != nil {
			return err
		}
		out.Stats.NetRevenue = out.Stats.TotalRevenue.Sub(out.Stats.TotalReturns)
		if err := tx.QueryRow(ctx, qDashCounts).Scan(&out.Stats.TotalCustomers, &out.Stats.TotalProducts); err != nil {
			return err
		}
		if out.RevenueData, err = months(ctx, tx, args, loc, from, to, a.d.Now()); err != nil {
			return err
		}
		out.SalesDistribution, err = topProducts(ctx, tx, args)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, out)
}

func months(ctx context.Context, tx tenancy.TenantTx, args []any, loc *time.Location, from, to *time.Time, now time.Time) ([]MonthPoint, error) {
	out := []MonthPoint{}
	first, last, ok := chartWindow(from, to, loc, now)
	if !ok {
		return out, nil
	}
	rows, err := tx.Query(ctx, qDashMonths, append(args, loc.String(), first.Format("2006-01-02"), last.Format("2006-01-02"))...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m MonthPoint
		if err := rows.Scan(&m.Month, &m.Revenue, &m.Orders); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func topProducts(ctx context.Context, tx tenancy.TenantTx, args []any) ([]Slice, error) {
	rows, err := tx.Query(ctx, qDashTop, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Slice{}
	for rows.Next() {
		var s Slice
		if err := rows.Scan(&s.Category, &s.Value); err != nil {
			return nil, err
		}
		s.Color = sliceColors[len(out)%len(sliceColors)]
		out = append(out, s)
	}
	return out, rows.Err()
}
