package batches

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Recalls and the trace report (industry P3 item 5, A31, design §5).
// A recalled batch stops selling at once: checkout reads the batch row
// with a share lock, and the recall takes it FOR UPDATE, so a sale in
// flight either finishes before the recall or sees it. Its stock can only
// be written off or quarantined; release is refused (A29).

type recallIn struct {
	Reason string `json:"reason"`
}

// recall is POST /api/batches/{id}/recall.
func (a *API) recall(w http.ResponseWriter, r *http.Request) { a.setRecall(w, r, true) }

// liftRecall is POST /api/batches/{id}/lift-recall.
func (a *API) liftRecall(w http.ResponseWriter, r *http.Request) { a.setRecall(w, r, false) }

func (a *API) setRecall(w http.ResponseWriter, r *http.Request, on bool) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in recallIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if n := len([]rune(in.Reason)); n < 5 || n > maxReasonLen {
		httpx.WriteError(w, r, apperr.Fields(map[string]string{"reason": fmt.Sprintf("Give a reason of 5 to %d characters.", maxReasonLen)}))
		return
	}
	p := principal(r)
	var out DetailJSON
	err = a.rw(r, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		var (
			status, batchNo string
			unknown         bool
		)
		err := tx.QueryRow(ctx, `SELECT status, batch_no::text, is_unknown FROM batches WHERE id = $1 FOR UPDATE`, id).Scan(&status, &batchNo, &unknown)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		action := "batch.recall"
		switch {
		case on && unknown:
			return apperr.Conflict(apperr.CodeInvalidState, "Stock with no batch cannot be recalled. Count it by batch first, or quarantine it.")
		case on && status == "recalled":
			return apperr.Conflict(apperr.CodeInvalidState, "This batch is already recalled.")
		case !on && status != "recalled":
			return apperr.Conflict(apperr.CodeInvalidState, "This batch is not recalled.")
		}
		if on {
			_, err = tx.Exec(ctx, `UPDATE batches SET status = 'recalled', recalled_at = now(), recall_reason = $2, updated_at = now() WHERE id = $1`, id, in.Reason)
		} else {
			action = "batch.lift_recall"
			_, err = tx.Exec(ctx, `UPDATE batches SET status = 'available', recalled_at = NULL, recall_reason = NULL, updated_at = now() WHERE id = $1`, id)
		}
		if err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: action, Entity: "batch", EntityID: strconv.FormatInt(id, 10),
			After:     map[string]any{"batchNo": batchNo, "reason": in.Reason},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		out, err = loadOne(ctx, tx, id)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// TraceSaleJSON is one sale of the batch. The customer is named only when
// it is not the walk-in customer.
type TraceSaleJSON struct {
	SaleID        int64     `json:"saleId"`
	Reference     string    `json:"reference"`
	Date          time.Time `json:"date"`
	Status        string    `json:"status"`
	StoreID       int64     `json:"storeId"`
	StoreName     string    `json:"storeName"`
	CustomerID    *int64    `json:"customerId"`
	CustomerName  *string   `json:"customerName"`
	CustomerPhone *string   `json:"customerPhone"`
	Quantity      qty.Q     `json:"quantity"`
}

// TraceReturnJSON is one return into the batch.
type TraceReturnJSON struct {
	ReturnID  int64     `json:"returnId"`
	Reference string    `json:"reference"`
	Date      time.Time `json:"date"`
	StoreName string    `json:"storeName"`
	Quantity  qty.Q     `json:"quantity"`
	Bucket    string    `json:"bucket"`
}

// TraceWriteOffJSON is one write-off from the batch.
type TraceWriteOffJSON struct {
	WriteOffID int64     `json:"writeOffId"`
	Reference  string    `json:"reference"`
	Date       time.Time `json:"date"`
	StoreName  string    `json:"storeName"`
	Reason     string    `json:"reason"`
	From       string    `json:"from"`
	Quantity   qty.Q     `json:"quantity"`
}

// TraceJSON is where a batch went (A31): for a supplier or regulator recall.
type TraceJSON struct {
	Batch      DetailJSON          `json:"batch"`
	Sales      []TraceSaleJSON     `json:"sales"`
	Returns    []TraceReturnJSON   `json:"returns"`
	WriteOffs  []TraceWriteOffJSON `json:"writeOffs"`
	Sold       qty.Q               `json:"sold"` // not counting void sales
	Returned   qty.Q               `json:"returned"`
	WrittenOff qty.Q               `json:"writtenOff"`
}

const (
	qTraceSales = `SELECT s.id, s.reference, s.date, s.status, coalesce(s.store_id, 0), coalesce(st.name, ''),
			CASE WHEN s.customer_id IS DISTINCT FROM (SELECT default_customer_id FROM settings WHERE id = 1) THEN s.customer_id END,
			CASE WHEN s.customer_id IS DISTINCT FROM (SELECT default_customer_id FROM settings WHERE id = 1) THEN c.name END,
			CASE WHEN s.customer_id IS DISTINCT FROM (SELECT default_customer_id FROM settings WHERE id = 1) THEN c.phone END,
			sum(ob.quantity)
		FROM order_item_batches ob JOIN order_items i ON i.id = ob.order_item_id JOIN sales s ON s.id = i.sale_id
		LEFT JOIN stores st ON st.id = s.store_id LEFT JOIN customers c ON c.id = s.customer_id
		WHERE ob.batch_id = $1
		GROUP BY s.id, st.name, c.name, c.phone ORDER BY s.date, s.id`
	qTraceReturns = `SELECT r.id, r.reference, r.date, coalesce(st.name, ''), sum(rb.quantity), rb.bucket
		FROM return_item_batches rb JOIN return_items ri ON ri.id = rb.return_item_id JOIN sale_returns r ON r.id = ri.sale_return_id
		LEFT JOIN stores st ON st.id = r.store_id
		WHERE rb.batch_id = $1 GROUP BY r.id, st.name, rb.bucket ORDER BY r.date, r.id`
	qTraceWriteOffs = `SELECT w.id, w.reference, w.created_at, st.name, w.reason, l.bucket, l.quantity
		FROM write_off_lines l JOIN write_offs w ON w.id = l.write_off_id JOIN stores st ON st.id = w.store_id
		WHERE l.batch_id = $1 ORDER BY w.created_at, w.id`
)

// trace is GET /api/batches/{id}/trace.
func (a *API) trace(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := TraceJSON{Sales: []TraceSaleJSON{}, Returns: []TraceReturnJSON{}, WriteOffs: []TraceWriteOffJSON{}}
	err = a.ro(r, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		var err error
		if out.Batch, err = loadOne(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, qTraceSales, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var s TraceSaleJSON
			if err := rows.Scan(&s.SaleID, &s.Reference, &s.Date, &s.Status, &s.StoreID, &s.StoreName, &s.CustomerID, &s.CustomerName,
				&s.CustomerPhone, &s.Quantity); err != nil {
				rows.Close()
				return err
			}
			if s.Status != "Void" {
				out.Sold = out.Sold.Add(s.Quantity)
			}
			out.Sales = append(out.Sales, s)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, qTraceReturns, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var x TraceReturnJSON
			if err := rows.Scan(&x.ReturnID, &x.Reference, &x.Date, &x.StoreName, &x.Quantity, &x.Bucket); err != nil {
				rows.Close()
				return err
			}
			out.Returned = out.Returned.Add(x.Quantity)
			out.Returns = append(out.Returns, x)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, qTraceWriteOffs, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var x TraceWriteOffJSON
			if err := rows.Scan(&x.WriteOffID, &x.Reference, &x.Date, &x.StoreName, &x.Reason, &x.From, &x.Quantity); err != nil {
				rows.Close()
				return err
			}
			out.WrittenOff = out.WrittenOff.Add(x.Quantity)
			out.WriteOffs = append(out.WriteOffs, x)
		}
		rows.Close()
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
