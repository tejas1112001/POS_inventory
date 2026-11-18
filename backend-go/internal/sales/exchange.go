package sales

// Exchanges (industry P1 item 9, A20): in one transaction, a return of
// goods from a sale and a new sale to the same customer at the same store.
// The return's value (after it reduces what was still owed on the sale,
// rule A) pays the new sale through payment method "exchange", linked to
// the return. If the new goods cost more, the customer pays the difference
// with the normal methods; if less, the rest is refunded or goes to store
// credit, as the policy allows. Each stock movement happens exactly once.

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/idempotency"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

const exchangeHashScope = "POST /api/sales/exchanges\n"

type exchangeIn struct {
	SaleID               money.Int      `json:"saleId"`
	ReturnLines          []returnItemIn `json:"returnLines"`
	NewLines             []itemIn       `json:"newLines"`
	Payments             []paymentIn    `json:"payments"`
	Settlement           *string        `json:"settlement"` // where a rest goes: refund (default) or store_credit
	PolicyOverrideReason *string        `json:"policyOverrideReason"`
	Note                 *string        `json:"note"`
	Date                 *string        `json:"date"`
	ReceiptEmail         *string        `json:"receiptEmail"`
}

// ExchangeQuote is the dry run's answer (?dryRun=true): the amounts the
// server would use. customerPays is what the normal payments must cover.
type ExchangeQuote struct {
	ReturnValue   decimal.Decimal `json:"returnValue"`
	AppliedToDue  decimal.Decimal `json:"appliedToDue"`
	NewTotal      decimal.Decimal `json:"newTotal"`
	ToExchange    decimal.Decimal `json:"toExchange"`
	CustomerPays  decimal.Decimal `json:"customerPays"`
	Refund        decimal.Decimal `json:"refund"`
	ToStoreCredit decimal.Decimal `json:"toStoreCredit"`
}

// ExchangeJSON is the A20 answer.
type ExchangeJSON struct {
	Return ReturnJSON `json:"return"`
	Sale   SaleJSON   `json:"sale"`
}

// errDryRun rolls the dry run's transaction back.
var errDryRun = errors.New("dry run")

func (a *API) exchange(w http.ResponseWriter, r *http.Request) {
	dry := r.URL.Query().Get("dryRun") == "true"
	key, err := idempotency.Key(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	hash := idempotency.Hash(r, exchangeHashScope)
	var in exchangeIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sum := hash()
	p := principal(r)
	if !p.Has("sales:create") {
		httpx.WriteError(w, r, apperr.Forbidden(apperr.CodeForbidden, "An exchange sells new goods, which needs the create sales permission."))
		return
	}
	f := fieldErrs{}
	if !in.SaleID.Valid || in.SaleID.V <= 0 {
		f["saleId"] = "saleId is required: an exchange starts from a sale."
	}
	if len(in.NewLines) == 0 {
		f["newLines"] = "Add the new goods the customer takes."
	}
	if in.Settlement != nil && *in.Settlement == SettleExchange {
		f["settlement"] = "settlement is where a rest goes: refund or store_credit."
	}
	cr, err := validateReturn(returnIn{ReturnItems: in.ReturnLines, SaleID: in.SaleID, Date: in.Date, Note: in.Note,
		Settlement: in.Settlement, PolicyOverrideReason: in.PolicyOverrideReason})
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			for k, v := range ae.Fields {
				f[renameReturnField(k)] = v
			}
		}
	}
	if err := f.err(); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	now := a.d.Now()
	var (
		out       ExchangeJSON
		quote     ExchangeQuote
		again     *idempotency.Replay
		receiptID int64
	)
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		if key != "" && !dry {
			var err error
			if again, err = idempotency.Claim(ctx, tx, p.UserID, key, sum); err != nil || again != nil {
				return err
			}
		}
		s, err := loadSettings(ctx, tx)
		if err != nil {
			return err
		}
		pol, err := loadPolicy(ctx, tx)
		if err != nil {
			return err
		}
		if err := needConditions(pol, cr, "returnLines"); err != nil {
			return err
		}
		pr, err := prepareReturn(ctx, tx, p, cr, s, now)
		if err != nil {
			return err
		}
		// The new sale: the same customer and store as the returned sale.
		payments := in.Payments
		if payments == nil {
			payments = []paymentIn{}
		}
		cust := pr.custID
		cs, err := validate(saleIn{OrderItems: in.NewLines, StoreID: money.Int{V: pr.storeID, Valid: true, Set: true},
			CustomerID: money.Int{V: cust, Valid: true, Set: true}, Payments: payments, Date: in.Date, ReceiptEmail: in.ReceiptEmail})
		if err != nil {
			return renameSaleFields(err)
		}
		byID, err := resolve(ctx, tx, cs.lines)
		if err != nil {
			return renameSaleFields(err)
		}
		prc, err := price(cs, byID, s, p, nil)
		if err != nil {
			return renameSaleFields(err)
		}
		newTotal := prc.quote.Total
		value := pr.total.Sub(pr.applied)
		toEx := decimal.Min(value, newTotal)
		rest := value.Sub(toEx)
		restMode := cr.settlement
		walkIn := s.defaultCustomer != nil && *s.defaultCustomer == cust
		if restMode == SettleStoreCredit && walkIn && rest.IsPositive() {
			return apperr.Invalid("settlement", "Store credit needs a named customer (not Walk-in).")
		}
		codes, err := enforce(p, []*violation{
			pol.windowClosed(pr.saleDate, now, s.loc),
			pol.settles(SettleExchange, decimal.NewFromInt(1)),
			pol.settles(restMode, rest),
		}, cr.overrideReason)
		if err != nil {
			return err
		}
		quote = ExchangeQuote{ReturnValue: pr.total, AppliedToDue: pr.applied, NewTotal: newTotal, ToExchange: toEx,
			CustomerPays: newTotal.Sub(toEx)}
		if restMode == SettleStoreCredit {
			quote.ToStoreCredit = rest
		} else {
			quote.Refund = rest
		}
		if dry {
			return errDryRun
		}

		// Stock: the returned goods come in (restocked lines only), the new
		// goods go out. Every row either touches is locked once, in order;
		// the shortage check uses the net change.
		pol.markRestocked(pr.rows)
		retDeltas := returnDeltas(pr)
		saleDeltas := map[inventory.Key]int{}
		for _, l := range prc.lines {
			saleDeltas[inventory.Key{Store: pr.storeID, Variant: l.v.id}] -= int(l.qty)
		}
		lockDeltas := map[inventory.Key]int{}
		for k, d := range retDeltas {
			lockDeltas[k] += d
		}
		for k, d := range saleDeltas {
			lockDeltas[k] += d
		}
		for k, d := range lockDeltas {
			if d == 0 {
				lockDeltas[k] = 1 // in and out again: lock it, nothing to check
			}
		}
		keys, _, err := inventory.Lock(ctx, tx, lockDeltas, skuMap(prc))
		if err != nil {
			return err
		}
		st := returnSettle{mode: SettleExchange, toExchange: toEx, payStatus: "Paid"}
		if restMode == SettleStoreCredit {
			st.toCredit = rest
		} else {
			st.paid = rest
		}
		retID, retRef, err := writeReturn(ctx, tx, p, cr, pr, st, keys, retDeltas)
		if err != nil {
			return err
		}
		if err := creditFromReturn(ctx, tx, s, cust, retID, p.UserID, st.toCredit); err != nil {
			return err
		}
		var ex *payLine
		if toEx.IsPositive() {
			ex = &payLine{method: MethodExchange, amount: toEx, returnID: &retID}
		}
		saleID, rid, saleRef, err := a.insertSale(r, tx, p, cs, s, now, saleExtra{priced: &prc, keys: keys, deltas: saleDeltas, exchange: ex})
		if err != nil {
			return err
		}
		receiptID = rid
		if _, err := tx.Exec(ctx, `UPDATE sales SET exchange_return_id = $2 WHERE id = $1`, saleID, retID); err != nil {
			return err
		}
		after := map[string]any{"return": retRef, "sale": saleRef, "return_value": pr.total, "applied_to_due": pr.applied,
			"to_exchange": toEx, "new_total": newTotal, "rest": rest, "rest_to": restMode}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "sale.exchange", Entity: "sale",
			EntityID: strconv.FormatInt(saleID, 10), After: after, RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		if err := auditOverride(r, tx, p, retID, retRef, codes, cr.overrideReason); err != nil {
			return err
		}
		rl, err := loadReturns(ctx, tx, []int64{retID})
		if err != nil || len(rl) == 0 {
			return orInternal(err)
		}
		sl, found, err := loadOne(ctx, tx, saleID)
		if err != nil || !found {
			return orInternal(err)
		}
		out = ExchangeJSON{Return: rl[0], Sale: sl}
		if key != "" {
			return idempotency.Store(ctx, tx, p.UserID, key, http.StatusCreated, out)
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		httpx.WriteJSON(w, http.StatusOK, quote)
		return
	}
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if again != nil {
		idempotency.Write(w, again)
		return
	}
	a.afterCommit(r, receiptID)
	httpx.WriteJSON(w, http.StatusCreated, out)
}

// renameReturnField maps a return field (returnItems[i]…) to the exchange
// body's name (returnLines[i]…).
func renameReturnField(k string) string {
	const from = "returnItems"
	if len(k) >= len(from) && k[:len(from)] == from {
		return "returnLines" + k[len(from):]
	}
	return k
}

// renameSaleFields maps the new sale's field errors (orderItems[i]…) to the
// exchange body's names (newLines[i]…).
func renameSaleFields(err error) error {
	var ae *apperr.Error
	if !errors.As(err, &ae) || len(ae.Fields) == 0 {
		return err
	}
	f := fieldErrs{}
	const from = "orderItems"
	for k, v := range ae.Fields {
		if len(k) >= len(from) && k[:len(from)] == from {
			k = "newLines" + k[len(from):]
		}
		f[k] = v
	}
	return f.err()
}
