import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, FileText, HandCoins, Loader2, Pencil, Undo2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { ErrorState } from '@/components/kit/States';
import StatusBadge from '@/components/kit/StatusBadge';
import { api } from '@/services/api';
import { formatCurrency, formatDate, formatDateTime } from '@/lib/format';
import { COLLECT_METHODS, formatDay, methodLabel, newKey } from '@/lib/payments';
import { toast } from '../../shared/toast/store';

// CustomerAccount (S10 item 5): what a customer owes per sale (oldest due
// first, overdue marked), the credit limit (customers:credit_limit to
// change it), Collect payment (customers:collect; paid to the oldest dues
// first) and the payments received, each reversible with a reason (a new
// negative entry; nothing is ever edited or deleted). Statement opens the
// printable statement.
export default function CustomerAccount() {
  const { id } = useParams();
  const [dues, setDues] = useState(null);
  const [payments, setPayments] = useState([]);
  const [error, setError] = useState('');
  const [dialog, setDialog] = useState(null); // 'collect' | 'limit' | {reverse: payment}

  const load = useCallback(() => {
    setError('');
    Promise.all([api.get(`/api/customers/${id}/dues`, { quiet: true }), api.get(`/api/customers/${id}/payments`, { quiet: true })])
      .then(([d, p]) => {
        setDues(d);
        setPayments(p || []);
      })
      .catch((err) => setError(err.status === 404 ? 'This customer does not exist.' : err.message));
  }, [id]);
  useEffect(load, [load]);

  const back = (
    <Link to="/customers" className="inline-flex items-center gap-1 text-sm text-primary hover:underline">
      <ArrowLeft className="size-4" aria-hidden="true" /> Customers
    </Link>
  );
  if (error) {
    return (
      <div className="space-y-3">
        {back}
        <ErrorState message={error} onRetry={load} />
      </div>
    );
  }
  if (!dues) return <div className="h-64 animate-pulse rounded-lg bg-muted" aria-busy="true" aria-label="Loading the account" />;

  const owes = Number(dues.totalDue) || 0;
  const done = (msg) => {
    setDialog(null);
    toast.success(msg);
    load();
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          {back}
          <h2 className="mt-1 text-xl font-semibold">{dues.name}</h2>
          <p className="text-sm text-muted-foreground">Account and dues</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button asChild variant="outline" className="max-sm:h-11">
            <Link to={`/customers/${id}/statement`}>
              <FileText className="size-4" aria-hidden="true" /> Statement
            </Link>
          </Button>
          {dues.canCollect && owes > 0 && (
            <Button type="button" className="max-sm:h-11" onClick={() => setDialog('collect')}>
              <HandCoins className="size-4" aria-hidden="true" /> Collect payment
            </Button>
          )}
        </div>
      </div>

      <section className="grid grid-cols-2 gap-3 md:grid-cols-4" aria-label="Summary">
        <Tile label="Owes now" value={formatCurrency(owes)} tone={owes > 0 ? 'text-danger-fg' : ''} />
        <Tile label="Overdue" value={formatCurrency(dues.overdueDue)} tone={Number(dues.overdueDue) > 0 ? 'text-danger-fg' : ''} />
        <Tile
          label="Credit limit"
          value={dues.walkIn ? 'Walk-in: no credit' : dues.creditLimit == null ? 'No credit' : formatCurrency(dues.creditLimit)}
          action={
            dues.canSetLimit && !dues.walkIn ? (
              <button type="button" onClick={() => setDialog('limit')} className="inline-flex items-center gap-1 text-xs font-medium text-primary hover:underline">
                <Pencil className="size-3" aria-hidden="true" /> Change
              </button>
            ) : null
          }
        />
        <Tile label="Credit left" value={dues.available == null ? '—' : formatCurrency(dues.available)} />
      </section>
      {Number(dues.owedByShop) > 0 && (
        <InlineAlert kind="info">The shop still owes {dues.name} {formatCurrency(dues.owedByShop)} from returns not yet refunded.</InlineAlert>
      )}

      <section className="overflow-hidden rounded-lg border bg-card shadow-xs" aria-labelledby="open-h">
        <h3 id="open-h" className="border-b px-4 py-3 text-base font-semibold">
          Unpaid sales <span className="font-normal text-muted-foreground">(oldest due first; payments pay these in this order)</span>
        </h3>
        {dues.sales.length === 0 ? (
          <p className="px-4 py-6 text-sm text-muted-foreground">Nothing owed.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="bg-muted text-left text-xs uppercase tracking-wide text-slate-600 dark:text-slate-300">
                <tr>
                  <th scope="col" className="px-4 py-2 font-medium">Sale</th>
                  <th scope="col" className="hidden px-3 py-2 font-medium sm:table-cell">Date</th>
                  <th scope="col" className="px-3 py-2 font-medium">Due by</th>
                  <th scope="col" className="hidden px-3 py-2 text-right font-medium md:table-cell">Total</th>
                  <th scope="col" className="hidden px-3 py-2 text-right font-medium md:table-cell">Paid</th>
                  <th scope="col" className="hidden px-3 py-2 text-right font-medium lg:table-cell">Returns</th>
                  <th scope="col" className="px-4 py-2 text-right font-medium">Due</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {dues.sales.map((s) => (
                  <tr key={s.id}>
                    <td className="px-4 py-2">
                      <Link to={`/sales/${s.id}`} className="font-medium text-primary hover:underline">
                        {s.reference}
                      </Link>
                    </td>
                    <td className="hidden px-3 py-2 sm:table-cell">{formatDate(s.date)}</td>
                    <td className="px-3 py-2">
                      {s.dueDate ? formatDay(s.dueDate) : '—'} {s.overdue && <StatusBadge value="Overdue" />}
                    </td>
                    <td className="hidden px-3 py-2 text-right tabular-nums md:table-cell">{formatCurrency(s.total)}</td>
                    <td className="hidden px-3 py-2 text-right tabular-nums md:table-cell">{formatCurrency(s.paid)}</td>
                    <td className="hidden px-3 py-2 text-right tabular-nums lg:table-cell">{Number(s.returned) > 0 ? formatCurrency(s.returned) : '—'}</td>
                    <td className="px-4 py-2 text-right font-medium tabular-nums">{formatCurrency(s.due)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="overflow-hidden rounded-lg border bg-card shadow-xs" aria-labelledby="pay-h">
        <h3 id="pay-h" className="border-b px-4 py-3 text-base font-semibold">
          Payments received against dues
        </h3>
        {payments.length === 0 ? (
          <p className="px-4 py-6 text-sm text-muted-foreground">None yet.</p>
        ) : (
          <ul className="divide-y">
            {payments.map((p) => {
              const reversal = p.reversalOf != null;
              return (
                <li key={p.id} className="flex flex-wrap items-start justify-between gap-2 px-4 py-3 text-sm">
                  <div className="min-w-0">
                    <p className="font-medium">
                      PAY-{p.id} · {reversal ? `Reversal of PAY-${p.reversalOf}` : methodLabel(p.method)}
                      {p.reference && <span className="font-normal text-muted-foreground"> · {p.reference}</span>}
                      {p.reversedBy != null && <span className="ml-2 text-xs font-normal text-warning-fg">reversed by PAY-{p.reversedBy}</span>}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {formatDateTime(p.createdAt)}
                      {p.userName ? ` · ${p.userName}` : ''}
                      {reversal && p.reason ? ` · ${p.reason}` : ''}
                      {p.note ? ` · ${p.note}` : ''}
                    </p>
                    <p className="text-xs text-muted-foreground">{p.allocations.map((a) => `${a.reference} ${formatCurrency(a.amount)}`).join(', ')}</p>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className={`font-semibold tabular-nums ${Number(p.amount) < 0 ? 'text-danger-fg' : ''}`}>{formatCurrency(p.amount)}</span>
                    {dues.canCollect && !reversal && p.reversedBy == null && (
                      <Button type="button" size="sm" variant="outline" onClick={() => setDialog({ reverse: p })}>
                        <Undo2 className="size-4" aria-hidden="true" /> Reverse
                      </Button>
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </section>

      {dialog === 'collect' && <CollectDialog id={id} dues={dues} onClose={() => setDialog(null)} onDone={() => done('Payment recorded.')} />}
      {dialog === 'limit' && <LimitDialog id={id} dues={dues} onClose={() => setDialog(null)} onDone={() => done('Credit limit saved.')} />}
      {dialog?.reverse && <ReverseDialog payment={dialog.reverse} onClose={() => setDialog(null)} onDone={() => done('Payment reversed.')} />}
    </div>
  );
}

function Tile({ label, value, tone = '', action = null }) {
  return (
    <div className="rounded-lg border bg-card px-4 py-3 shadow-xs">
      <div className="flex items-center justify-between gap-2">
        <p className="text-xs font-medium text-muted-foreground">{label}</p>
        {action}
      </div>
      <p className={`mt-1 text-lg font-semibold tabular-nums ${tone}`}>{value}</p>
    </div>
  );
}

// Small dialog frame shared by the three actions.
function ActionDialog({ title, description, error, busy, submitLabel, onClose, onSubmit, children, destructive = false }) {
  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()}>
      <DialogContent className="sm:max-w-md">
        <form
          noValidate
          className="contents"
          onSubmit={(e) => {
            e.preventDefault();
            onSubmit();
          }}
        >
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </DialogHeader>
          {error && <InlineAlert>{error}</InlineAlert>}
          <div className="space-y-3">{children}</div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose} disabled={busy}>
              Cancel
            </Button>
            <Button type="submit" variant={destructive ? 'destructive' : 'default'} disabled={busy}>
              {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
              {submitLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function useAction(fn, onDone) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const run = async () => {
    if (busy) return;
    setBusy(true);
    setError('');
    try {
      await fn();
      onDone();
    } catch (err) {
      setError(err.fields ? Object.values(err.fields).join(' ') : err.message);
    } finally {
      setBusy(false);
    }
  };
  return { busy, error, setError, run };
}

function CollectDialog({ id, dues, onClose, onDone }) {
  const [amount, setAmount] = useState(String(Number(dues.totalDue).toFixed(2)));
  const [method, setMethod] = useState('cash');
  const [reference, setReference] = useState('');
  const [note, setNote] = useState('');
  const key = useRef(newKey()); // the same key on a retry: never recorded twice
  const a = useAction(
    () =>
      api.post(
        `/api/customers/${id}/payments`,
        { amount: Number(amount), method, ...(reference.trim() ? { reference: reference.trim() } : {}), ...(note.trim() ? { note: note.trim() } : {}) },
        { headers: { 'Idempotency-Key': key.current }, quiet: true }
      ),
    onDone
  );
  return (
    <ActionDialog
      title={`Collect payment from ${dues.name}`}
      description={`Owes ${formatCurrency(dues.totalDue)}. The payment pays the oldest dues first.`}
      error={a.error}
      busy={a.busy}
      submitLabel="Record payment"
      onClose={onClose}
      onSubmit={() => {
        if (!(Number(amount) > 0)) return a.setError('Enter an amount above 0.');
        if (Number(amount) > Number(dues.totalDue) + 0.001) return a.setError(`${dues.name} owes ${formatCurrency(dues.totalDue)}; take at most that.`);
        a.run();
      }}
    >
      <label className="block space-y-1 text-sm font-medium">
        Amount
        <Input inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value.replace(/[^0-9.]/g, ''))} className="h-11 text-right text-lg tabular-nums" />
      </label>
      <label className="block space-y-1 text-sm font-medium">
        Method
        <select value={method} onChange={(e) => setMethod(e.target.value)} className="h-10 w-full rounded-md border border-input bg-card px-3 text-sm">
          {COLLECT_METHODS.map((m) => (
            <option key={m} value={m}>
              {methodLabel(m)}
            </option>
          ))}
        </select>
      </label>
      <label className="block space-y-1 text-sm font-medium">
        Reference <span className="font-normal text-muted-foreground">(optional: UPI ID, cheque number…)</span>
        <Input value={reference} maxLength={100} onChange={(e) => setReference(e.target.value)} />
      </label>
      <label className="block space-y-1 text-sm font-medium">
        Note <span className="font-normal text-muted-foreground">(optional)</span>
        <Input value={note} maxLength={500} onChange={(e) => setNote(e.target.value)} />
      </label>
    </ActionDialog>
  );
}

function LimitDialog({ id, dues, onClose, onDone }) {
  const [none, setNone] = useState(dues.creditLimit == null);
  const [limit, setLimit] = useState(dues.creditLimit == null ? '' : String(dues.creditLimit));
  const a = useAction(() => api.put(`/api/customers/${id}/credit-limit`, { creditLimit: none ? null : Number(limit) }, { quiet: true }), onDone);
  return (
    <ActionDialog
      title={`Credit limit for ${dues.name}`}
      description="The most this customer may owe in total. Sales on account that would pass it are refused at the till."
      error={a.error}
      busy={a.busy}
      submitLabel="Save"
      onClose={onClose}
      onSubmit={() => {
        if (!none && !(Number(limit) >= 0 && limit.trim() !== '')) return a.setError('Enter an amount, or choose no credit.');
        a.run();
      }}
    >
      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={none} onChange={(e) => setNone(e.target.checked)} className="size-4" /> No credit for this customer
      </label>
      {!none && (
        <label className="block space-y-1 text-sm font-medium">
          Limit
          <Input inputMode="decimal" value={limit} onChange={(e) => setLimit(e.target.value.replace(/[^0-9.]/g, ''))} className="h-11 text-right text-lg tabular-nums" />
        </label>
      )}
      {Number(dues.totalDue) > 0 && <p className="text-xs text-muted-foreground">Owes {formatCurrency(dues.totalDue)} now.</p>}
    </ActionDialog>
  );
}

function ReverseDialog({ payment, onClose, onDone }) {
  const [reason, setReason] = useState('');
  const a = useAction(() => api.post(`/api/customer-payments/${payment.id}/reverse`, { reason: reason.trim() }, { quiet: true }), onDone);
  return (
    <ActionDialog
      title={`Reverse PAY-${payment.id}?`}
      description={`${formatCurrency(payment.amount)} goes back onto the sales it paid. A reversal entry is added; the payment itself stays in the history.`}
      error={a.error}
      busy={a.busy}
      submitLabel="Reverse payment"
      destructive
      onClose={onClose}
      onSubmit={() => {
        if (reason.trim().length < 5) return a.setError('Give a reason (at least 5 characters).');
        a.run();
      }}
    >
      <label className="block space-y-1 text-sm font-medium">
        Reason
        <Input value={reason} maxLength={500} onChange={(e) => setReason(e.target.value)} placeholder="For example: cheque bounced" />
      </label>
    </ActionDialog>
  );
}
