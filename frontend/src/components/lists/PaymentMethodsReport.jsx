import { useEffect, useState } from 'react';
import { api } from '@/services/api';
import { formatCurrency, todayInShop } from '@/lib/format';
import { addDays, methodLabel } from '@/lib/payments';
import { InlineAlert } from '@/components/feedback/InlineAlert';

// PaymentMethodsReport (S10 item 5): money taken per payment method in a
// period, at the till and as payments against dues (reversals subtract).
// "On account" is what was sold on credit. Own sales only without
// sales:view_all, like the sales report.
export default function PaymentMethodsReport() {
  const [from, setFrom] = useState(addDays(todayInShop(), -29));
  const [to, setTo] = useState(todayInShop());
  const [rows, setRows] = useState(null);
  const [error, setError] = useState('');

  useEffect(() => {
    let live = true;
    setRows(null);
    setError('');
    api
      .get(`/api/sales/report/payment-methods?startDate=${from}&endDate=${to}`, { quiet: true })
      .then((r) => live && setRows(r || []))
      .catch((err) => live && setError(err.message));
    return () => {
      live = false;
    };
  }, [from, to]);

  const taken = (rows || []).filter((r) => r.method !== 'credit').reduce((s, r) => s + Number(r.amount), 0);

  return (
    <section className="rounded-lg border bg-card shadow-xs" aria-labelledby="pm-h">
      <div className="flex flex-wrap items-end justify-between gap-2 border-b px-4 py-3">
        <h2 id="pm-h" className="text-base font-semibold">
          Sales by payment method
        </h2>
        <div className="flex items-end gap-2 text-xs">
          <label className="space-y-1 font-medium">
            From
            <input type="date" className="block h-9 rounded-md border bg-card px-2" value={from} max={to} onChange={(e) => e.target.value && setFrom(e.target.value)} />
          </label>
          <label className="space-y-1 font-medium">
            To
            <input type="date" className="block h-9 rounded-md border bg-card px-2" value={to} min={from} onChange={(e) => e.target.value && setTo(e.target.value)} />
          </label>
        </div>
      </div>
      {error && <InlineAlert>{error}</InlineAlert>}
      {rows && rows.length === 0 && <p className="px-4 py-6 text-sm text-muted-foreground">No payments in this period.</p>}
      {rows && rows.length > 0 && (
        <table className="w-full text-sm">
          <thead className="bg-muted text-left text-xs uppercase tracking-wide text-slate-600 dark:text-slate-300">
            <tr>
              <th scope="col" className="px-4 py-2 font-medium">Method</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">Payments</th>
              <th scope="col" className="hidden px-3 py-2 text-right font-medium sm:table-cell">Of which dues collected</th>
              <th scope="col" className="px-4 py-2 text-right font-medium">Amount</th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {rows.map((r) => (
              <tr key={r.method}>
                <td className="px-4 py-2">{methodLabel(r.method)}</td>
                <td className="px-3 py-2 text-right tabular-nums">{r.payments}</td>
                <td className="hidden px-3 py-2 text-right tabular-nums sm:table-cell">{Number(r.collections) ? formatCurrency(r.collections) : '—'}</td>
                <td className="px-4 py-2 text-right font-medium tabular-nums">{formatCurrency(r.amount)}</td>
              </tr>
            ))}
          </tbody>
          <tfoot className="border-t">
            <tr>
              <td className="px-4 py-2 font-semibold" colSpan={3}>
                Money taken (without on account)
              </td>
              <td className="px-4 py-2 text-right font-bold tabular-nums">{formatCurrency(taken)}</td>
            </tr>
          </tfoot>
        </table>
      )}
    </section>
  );
}
