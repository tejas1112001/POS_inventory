import { useEffect, useState } from 'react';
import { ChevronDown } from 'lucide-react';
import { api } from '@/services/api';
import { formatCurrency } from '@/lib/format';
import { isRecorded, methodLabel, RECORDED_HINT } from '@/lib/payments';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { cn } from '@/lib/utils';

const OPEN_KEY = 'salesReport.paymentMethodsOpen';
const readOpen = () => {
  try {
    return localStorage.getItem(OPEN_KEY) === '1';
  } catch {
    return false;
  }
};

// PaymentMethodsReport (S10 item 5): money taken per payment method, at the
// till and as payments against dues (reversals subtract). "On account" is
// what was sold on credit. It uses the Sales tab's date range and store
// (qs), so it always covers the same period as the cards and the table.
// Own sales only without sales:view_all, like the sales report. Folded by
// default (the heading shows the money taken) so the table keeps its room.
export default function PaymentMethodsReport({ qs }) {
  const [rows, setRows] = useState(null);
  const [error, setError] = useState('');
  const [open, setOpen] = useState(readOpen);

  useEffect(() => {
    let live = true;
    setRows(null);
    setError('');
    api
      .get(`/api/sales/report/payment-methods${qs}`, { quiet: true })
      .then((r) => live && setRows(r || []))
      .catch((err) => live && setError(err.message));
    return () => {
      live = false;
    };
  }, [qs]);

  const toggle = () => {
    setOpen((o) => {
      try {
        localStorage.setItem(OPEN_KEY, o ? '0' : '1');
      } catch {
        /* remembered for this visit only */
      }
      return !o;
    });
  };

  const taken = (rows || []).filter((r) => r.method !== 'credit').reduce((s, r) => s + Number(r.amount), 0);
  const showCollections = (rows || []).some((r) => Number(r.collections));
  const anyRecorded = (rows || []).some((r) => isRecorded(r.method));

  return (
    <section className="mb-3 shrink-0 rounded-2xl border bg-card shadow-xs">
      <h2 className="text-base font-semibold">
        <button
          type="button"
          onClick={toggle}
          aria-expanded={open}
          aria-controls="pm-body"
          className="flex w-full items-center gap-2 rounded-2xl px-4 py-3 text-left hover:bg-accent/40"
        >
          <ChevronDown className={cn('size-4 shrink-0 text-muted-foreground transition-transform', !open && '-rotate-90')} aria-hidden="true" />
          <span className="flex-1">Sales by payment method</span>
          <span className="text-sm font-medium text-muted-foreground">
            Money taken <span className="font-bold text-foreground tabular-nums">{rows ? formatCurrency(taken) : '…'}</span>
          </span>
        </button>
      </h2>
      {open && (
        <div id="pm-body" className="border-t">
          {error && <InlineAlert>{error}</InlineAlert>}
          {rows && rows.length === 0 && <p className="px-4 py-6 text-sm text-muted-foreground">No payments in this period.</p>}
          {rows && rows.length > 0 && (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-muted text-left text-xs font-semibold text-muted-foreground">
                  <tr>
                    <th scope="col" className="px-4 py-2">Method</th>
                    <th scope="col" className="px-3 py-2 text-right">Payments</th>
                    {showCollections && <th scope="col" className="w-36 px-3 py-2 text-right">Dues collected</th>}
                    <th scope="col" className="w-40 px-4 py-2 text-right">Amount</th>
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {rows.map((r) => (
                    <tr key={r.method}>
                      <td className="px-4 py-2" title={isRecorded(r.method) ? RECORDED_HINT : undefined}>
                        {methodLabel(r.method)}
                        {isRecorded(r.method) && <span className="text-muted-foreground" aria-label="recorded"> *</span>}
                      </td>
                      <td className="px-3 py-2 text-right tabular-nums">{r.payments}</td>
                      {showCollections && (
                        <td className="px-3 py-2 text-right tabular-nums">{Number(r.collections) ? formatCurrency(r.collections) : '—'}</td>
                      )}
                      <td className="px-4 py-2 text-right font-medium tabular-nums">{formatCurrency(r.amount)}</td>
                    </tr>
                  ))}
                </tbody>
                <tfoot className="border-t">
                  <tr>
                    <td className="px-4 py-2 font-semibold" colSpan={showCollections ? 3 : 2}>
                      Money taken (without on account)
                    </td>
                    <td className="px-4 py-2 text-right font-bold tabular-nums">{formatCurrency(taken)}</td>
                  </tr>
                </tfoot>
              </table>
            </div>
          )}
          {anyRecorded && <p className="border-t px-4 py-2 text-xs text-muted-foreground">* Recorded by the cashier, not verified by a bank or payment provider.</p>}
        </div>
      )}
    </section>
  );
}
