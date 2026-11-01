package reports

import (
	"context"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
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
	Extra             DashboardExtra   `json:"extra"`
}

// PeriodFigures are one period's sales figures (UI S7).
type PeriodFigures struct {
	Revenue   decimal.Decimal `json:"revenue"`
	Returns   decimal.Decimal `json:"returns"`
	Net       decimal.Decimal `json:"net"`
	Paid      decimal.Decimal `json:"paid"`
	Due       decimal.Decimal `json:"due"`
	Discounts decimal.Decimal `json:"discounts"`
	Orders    int             `json:"orders"`
	ItemsSold int64           `json:"itemsSold"`
}

// SeriesPoint is one bar or point of the sales chart.
type SeriesPoint struct {
	Label   string          `json:"label"`
	Revenue decimal.Decimal `json:"revenue"`
	Orders  int             `json:"orders"`
}

// DashboardExtra is what the S7 dashboard adds. Counts are nil when the
// user may not open the page they lead to.
type DashboardExtra struct {
	Current           PeriodFigures  `json:"current"`
	Previous          *PeriodFigures `json:"previous"` // the period of the same length just before; nil without both dates
	PreviousStart     *time.Time     `json:"previousStart"`
	PreviousEnd       *time.Time     `json:"previousEnd"`
	Granularity       string         `json:"granularity"` // hour, day or month
	Series            []SeriesPoint  `json:"series"`
	LowStock          *int           `json:"lowStock"`
	OutOfStock        *int           `json:"outOfStock"`
	PurchasesWaiting  *int           `json:"purchasesWaiting"`
	CustomersWithDues *int           `json:"customersWithDues"`
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
	qDashPeriod = `SELECT coalesce(sum(s.total), 0), count(*), coalesce(sum(s.paid), 0), coalesce(sum(s.due), 0),
			coalesce(sum(s.discount), 0)
		FROM sales s WHERE ` + qScope
	qDashItems = `SELECT coalesce(sum(i.quantity), 0) FROM order_items i JOIN sales s ON s.id = i.sale_id WHERE ` + qScope
	// Hours or days in the tenant's time zone ($5) from $3 to $4, zero-filled.
	qDashSeries = `SELECT to_char(b, $6), coalesce(sum(s.total), 0), count(s.id)
		FROM generate_series(date_trunc($7, $3::timestamptz AT TIME ZONE $5::text),
			date_trunc($7, ($4::timestamptz - interval '1 microsecond') AT TIME ZONE $5::text), ('1 ' || $7)::interval) b
		LEFT JOIN sales s ON date_trunc($7, s.date AT TIME ZONE $5::text) = b AND ` + qScope + `
		GROUP BY b ORDER BY b`
	// Same rule as the Stock page's Low Stock / Out of Stock filters.
	qDashStock = `SELECT count(*) FILTER (WHERE i.qty > 0 AND i.qty <= i.quantity_alert), count(*) FILTER (WHERE i.qty <= 0)
		FROM inventories i
		JOIN product_variants v ON v.id = i.variant_id AND v.deleted_at IS NULL
		JOIN products p ON p.id = v.product_id AND p.deleted_at IS NULL
		JOIN stores st ON st.id = i.store_id AND st.deleted_at IS NULL AND st.status = 'Active'
		WHERE ($1 = 0 OR i.store_id = $1)`
	qDashPurchases = `SELECT count(*) FROM purchases WHERE status IN ('Ordered','Pending')`
	qDashDues      = `SELECT count(DISTINCT s.customer_id) FROM sales s WHERE s.status <> 'Void' AND s.due > 0 AND ($1 = 0 OR s.store_id = $1)`
	qDashCounts    = `SELECT (SELECT count(*) FROM customers WHERE deleted_at IS NULL),
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
		if out.SalesDistribution, err = topProducts(ctx, tx, args); err != nil {
			return err
		}
		return extras(ctx, tx, p, &out, scope, store, from, to, loc)
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

// periodFigures reads one period's figures.
func periodFigures(ctx context.Context, tx tenancy.TenantTx, args []any) (PeriodFigures, error) {
	var f PeriodFigures
	if err := tx.QueryRow(ctx, qDashPeriod, args...).Scan(&f.Revenue, &f.Orders, &f.Paid, &f.Due, &f.Discounts); err != nil {
		return f, err
	}
	if err := tx.QueryRow(ctx, qDashReturns, args...).Scan(&f.Returns); err != nil {
		return f, err
	}
	if err := tx.QueryRow(ctx, qDashItems, args...).Scan(&f.ItemsSold); err != nil {
		return f, err
	}
	f.Net = f.Revenue.Sub(f.Returns)
	return f, nil
}

// extras fills the S7 additions: both periods, the chart series and the
// action-card counts (each only for users who may open its page).
func extras(ctx context.Context, tx tenancy.TenantTx, p auth.Principal, out *Dashboard, scope, store int64, from, to *time.Time, loc *time.Location) error {
	x := &out.Extra
	var err error
	if x.Current, err = periodFigures(ctx, tx, []any{scope, store, from, to}); err != nil {
		return err
	}
	if from != nil && to != nil && to.After(*from) {
		ps, pe := from.Add(-to.Sub(*from)), *from
		prev, err := periodFigures(ctx, tx, []any{scope, store, &ps, &pe})
		if err != nil {
			return err
		}
		x.Previous, x.PreviousStart, x.PreviousEnd = &prev, &ps, &pe
	}
	x.Granularity, x.Series = "month", []SeriesPoint{}
	for _, m := range out.RevenueData {
		x.Series = append(x.Series, SeriesPoint{Label: m.Month, Revenue: m.Revenue, Orders: m.Orders})
	}
	if from != nil && to != nil && to.After(*from) {
		unit, layout := "", ""
		switch span := to.Sub(*from); {
		case span <= 26*time.Hour:
			unit, layout = "hour", "HH24:00"
		case span <= 62*24*time.Hour:
			unit, layout = "day", "DD Mon"
		}
		if unit != "" {
			rows, err := tx.Query(ctx, qDashSeries, scope, store, from, to, loc.String(), layout, unit)
			if err != nil {
				return err
			}
			defer rows.Close()
			x.Granularity, x.Series = unit, []SeriesPoint{}
			for rows.Next() {
				var sp SeriesPoint
				if err := rows.Scan(&sp.Label, &sp.Revenue, &sp.Orders); err != nil {
					return err
				}
				x.Series = append(x.Series, sp)
			}
			if err := rows.Err(); err != nil {
				return err
			}
		}
	}
	if p.Has("inventory:view") {
		var low, out int
		if err := tx.QueryRow(ctx, qDashStock, store).Scan(&low, &out); err != nil {
			return err
		}
		x.LowStock, x.OutOfStock = &low, &out
	}
	if p.Has("purchases:view") {
		var n int
		if err := tx.QueryRow(ctx, qDashPurchases).Scan(&n); err != nil {
			return err
		}
		x.PurchasesWaiting = &n
	}
	if p.Has("reports:view") && p.Has("sales:view_all") {
		var n int
		if err := tx.QueryRow(ctx, qDashDues, store).Scan(&n); err != nil {
			return err
		}
		x.CustomersWithDues = &n
	}
	return nil
}
