import { useCallback, useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, Printer } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ErrorState } from '@/components/kit/States';
import { api } from '@/services/api';
import { useShopSettings } from '@/hooks/useShopSettings';
import { formatCurrency, formatDate, todayInShop } from '@/lib/format';
import { addDays, formatDay } from '@/lib/payments';

// CustomerStatement (S10 item 5): sales, payments, returns and the running
// balance for a date range, with the opening balance carried in. Printed
// with the browser (A4); the toolbar does not print.
export default function CustomerStatement() {
  const { id } = useParams();
  const { settings } = useShopSettings();
  const [from, setFrom] = useState(addDays(todayInShop(), -90));
  const [to, setTo] = useState(todayInShop());
  const [st, setSt] = useState(null);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    setError('');
    api
      .get(`/api/customers/${id}/statement?startDate=${from}&endDate=${to}`, { quiet: true })
      .then(setSt)
      .catch((err) => setError(err.status === 404 ? 'This customer does not exist.' : err.message));
  }, [id, from, to]);
  useEffect(load, [load]);

  return (
    <div className="mx-auto max-w-4xl space-y-4 bg-background p-4 text-foreground print:max-w-none print:p-0">
      <style>{'@page { size: A4; margin: 14mm; }'}</style>
      <div className="flex flex-wrap items-end justify-between gap-3 print:hidden">
        <Link to={`/customers/${id}`} className="inline-flex items-center gap-1 text-sm text-primary hover:underline">
          <ArrowLeft className="size-4" aria-hidden="true" /> Account
        </Link>
        <div className="flex flex-wrap items-end gap-2">
          <label className="space-y-1 text-xs font-medium">
            From
            <Input type="date" value={from} max={to} onChange={(e) => e.target.value && setFrom(e.target.value)} className="h-9" />
          </label>
          <label className="space-y-1 text-xs font-medium">
            To
            <Input type="date" value={to} min={from} onChange={(e) => e.target.value && setTo(e.target.value)} className="h-9" />
          </label>
          <Button type="button" onClick={() => window.print()} disabled={!st}>
            <Printer className="size-4" aria-hidden="true" /> Print
          </Button>
        </div>
      </div>

      {error && <ErrorState message={error} onRetry={load} />}
      {st && (
        <article className="rounded-lg border bg-card p-6 print:border-0 print:p-0" aria-label="Statement">
          <header className="flex flex-wrap justify-between gap-4 border-b pb-4">
            <div>
              <h1 className="text-xl font-bold">{settings?.businessName || 'Statement'}</h1>
              {settings?.address && <p className="whitespace-pre-line text-sm text-muted-foreground">{settings.address}</p>}
            </div>
            <div className="text-right text-sm">
              <p className="text-lg font-semibold">Customer statement</p>
              <p>{st.name}</p>
              <p className="text-muted-foreground">
                {formatDay(from)} – {formatDay(to)}
              </p>
            </div>
          </header>

          <table className="mt-4 w-full text-sm">
            <thead className="border-b text-left text-xs uppercase tracking-wide text-muted-foreground">
              <tr>
                <th scope="col" className="py-2 pr-2 font-medium">Date</th>
                <th scope="col" className="py-2 pr-2 font-medium">Reference</th>
                <th scope="col" className="py-2 pr-2 font-medium">Details</th>
                <th scope="col" className="py-2 pr-2 text-right font-medium">Charged</th>
                <th scope="col" className="py-2 pr-2 text-right font-medium">Paid / credited</th>
                <th scope="col" className="py-2 text-right font-medium">Balance</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              <tr>
                <td className="py-2 pr-2" colSpan={5}>
                  Opening balance
                </td>
                <td className="py-2 text-right font-medium tabular-nums">{formatCurrency(st.opening)}</td>
              </tr>
              {st.lines.map((l, i) => (
                <tr key={i} className="break-inside-avoid">
                  <td className="py-1.5 pr-2 whitespace-nowrap">{formatDate(l.date)}</td>
                  <td className="py-1.5 pr-2">{l.reference}</td>
                  <td className="py-1.5 pr-2">{l.description}</td>
                  <td className="py-1.5 pr-2 text-right tabular-nums">{Number(l.debit) ? formatCurrency(l.debit) : ''}</td>
                  <td className="py-1.5 pr-2 text-right tabular-nums">{Number(l.credit) ? formatCurrency(l.credit) : ''}</td>
                  <td className="py-1.5 text-right tabular-nums">{formatCurrency(l.balance)}</td>
                </tr>
              ))}
            </tbody>
            <tfoot className="border-t-2">
              <tr>
                <td className="py-2 pr-2 font-semibold" colSpan={5}>
                  {Number(st.closing) < 0 ? 'The shop owes the customer' : 'Balance due'}
                </td>
                <td className="py-2 text-right text-base font-bold tabular-nums">{formatCurrency(Math.abs(Number(st.closing)))}</td>
              </tr>
            </tfoot>
          </table>
          {st.lines.length === 0 && <p className="mt-3 text-sm text-muted-foreground">Nothing in this period.</p>}
        </article>
      )}
    </div>
  );
}
