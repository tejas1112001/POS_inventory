import { useEffect, useRef, useState } from 'react';
import { Banknote, CheckCircle2, CreditCard, Delete, Landmark, Loader2, NotebookPen, Plus, ScrollText, Smartphone, Star, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { formatCurrency, todayInShop } from '@/lib/format';
import { addDays, methodLabel } from '@/lib/payments';
import { cn } from '@/lib/utils';
import { parseAmount } from './useCheckout';

// Payment methods (S10 item 5): the values are what the server stores.
const TYPES = [
  { value: 'cash', label: 'Cash', icon: Banknote },
  { value: 'upi', label: 'UPI', icon: Smartphone },
  { value: 'card', label: 'Card', icon: CreditCard },
  { value: 'cheque', label: 'Cheque', icon: ScrollText },
  { value: 'deposit', label: 'Deposit', icon: Landmark },
  { value: 'points', label: 'Points', icon: Star },
];
const WITH_REF = { upi: 'UPI transaction ID', card: 'Card last 4 digits', cheque: 'Cheque number', deposit: 'Bank reference' };
const NOTES = [10, 20, 50, 100, 200, 500, 2000];
const KEYS = ['1', '2', '3', '4', '5', '6', '7', '8', '9', '.', '0', 'del'];
const round2 = (v) => Math.round(v * 100) / 100;

// PaymentSheet takes one payment (the quick path: pick a method, Complete)
// or several (Add payment for each), and, for a named customer and a
// cashier allowed to sell on credit, puts the rest on account with a due
// date. The server checks the credit limit and answers "Credit limit
// reached: …" when it is passed; the sheet stays open with that message.
// Pressing Complete again reuses the same sale key (never charged twice).
export default function PaymentSheet({ open, onClose, total, customerEmail = '', onPay, onAccount = null }) {
  const [type, setType] = useState('cash');
  const [entry, setEntry] = useState(''); // cash received, or the amount for other methods
  const [reference, setReference] = useState('');
  const [lines, setLines] = useState([]); // {method, amount, tendered?, reference}
  const [rest, setRest] = useState(false); // the rest on account
  const [dueDate, setDueDate] = useState('');
  const [email, setEmail] = useState(customerEmail);
  const [notesOpen, setNotesOpen] = useState(false);
  const [paymentNote, setPaymentNote] = useState('');
  const [saleNote, setSaleNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const entryRef = useRef(null);

  useEffect(() => {
    if (!open) return;
    setEntry('');
    setReference('');
    setLines([]);
    setRest(false);
    setDueDate(addDays(todayInShop(), 30));
    setError('');
    setEmail(customerEmail);
  }, [open, customerEmail]);

  const cash = type === 'cash';
  const paidSoFar = round2(lines.reduce((s, l) => s + l.amount, 0));
  const left = round2(Math.max(total - paidSoFar, 0));
  const value = parseAmount(entry);
  const change = cash && entry !== '' ? round2(value - left) : 0;

  const press = (k) => {
    setError('');
    setEntry((r) => {
      if (k === 'del') return r.slice(0, -1);
      if (k === '.' && r.includes('.')) return r;
      if (/\.\d{2}$/.test(r)) return r;
      return `${r}${k}`;
    });
  };

  // current turns what is typed into a payment line (null: nothing typed).
  const current = () => {
    if (entry.trim() === '') return cash ? null : left > 0 ? { method: type, amount: left, reference: reference.trim() } : null;
    if (!(value > 0)) throw new Error('Enter an amount above 0.');
    if (cash) return { method: 'cash', amount: round2(Math.min(value, left)), tendered: value, reference: '' };
    if (value > left + 0.001) throw new Error(`Only ${formatCurrency(left)} is left to pay; give change on cash instead.`);
    return { method: type, amount: round2(value), reference: reference.trim() };
  };

  const addPayment = () => {
    setError('');
    try {
      const l = current();
      if (!l || !(l.amount > 0)) {
        setError(cash ? 'Enter the cash received.' : 'Nothing is left to pay.');
        return;
      }
      setLines((ls) => [...ls, l]);
      setEntry('');
      setReference('');
      entryRef.current?.focus();
    } catch (err) {
      setError(err.message);
    }
  };

  const submit = async (e) => {
    e.preventDefault();
    if (busy) return;
    let all = lines;
    try {
      const l = current();
      if (l && l.amount > 0) all = [...lines, l];
    } catch (err) {
      setError(err.message);
      return;
    }
    const paid = round2(all.reduce((s, l) => s + l.amount, 0));
    const short = round2(total - paid);
    if (short > 0.004 && !rest) {
      setError(
        all.length === 0 && cash
          ? 'Enter the cash received (or press "Exact").'
          : `${formatCurrency(short)} is still to pay. Add another payment${onAccount ? ' or put the rest on account' : ''}.`
      );
      entryRef.current?.focus();
      return;
    }
    if (rest && !/^\d{4}-\d{2}-\d{2}$/.test(dueDate)) {
      setError('Choose the date the rest is due.');
      return;
    }
    if (email.trim() && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) {
      setError('Enter a valid email address, or leave it empty.');
      return;
    }
    const payments = all.map((l) => ({
      method: l.method,
      amount: l.amount,
      ...(l.tendered ? { tendered: l.tendered } : {}),
      ...(l.reference ? { reference: l.reference } : {}),
    }));
    if (short > 0.004) payments.push({ method: 'credit' });
    setBusy(true);
    setError('');
    try {
      await onPay({ payments, ...(short > 0.004 ? { dueDate } : {}), paymentNote, saleNote, receiptEmail: email.trim() });
    } catch (err) {
      setError(err.message || 'The sale could not be saved.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && !busy && onClose()}>
      <DialogContent
        sheet
        className="sm:max-w-3xl"
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          entryRef.current?.focus();
        }}
      >
        <form onSubmit={submit} noValidate className="contents">
          <DialogHeader>
            <DialogTitle>Take payment</DialogTitle>
            <DialogDescription className="sr-only">Payments, cash received, change, the rest on account and the receipt email.</DialogDescription>
          </DialogHeader>

          {error && <InlineAlert>{error}</InlineAlert>}

          <div className="grid gap-5 md:grid-cols-2">
            <div className="space-y-3">
              <div className="grid grid-cols-2 gap-2">
                <div className="rounded-lg bg-accent px-4 py-3">
                  <p className="text-xs font-medium text-accent-foreground">Total</p>
                  <p className="text-2xl font-bold tabular-nums text-navy">{formatCurrency(total)}</p>
                </div>
                <div className={cn('rounded-lg px-4 py-3', left > 0 ? 'bg-warning-bg' : 'bg-success-bg')}>
                  <p className="text-xs font-medium">Left to pay</p>
                  <p className="text-2xl font-bold tabular-nums">{formatCurrency(left)}</p>
                </div>
              </div>

              {lines.length > 0 && (
                <ul className="divide-y rounded-lg border bg-card text-sm" aria-label="Payments so far">
                  {lines.map((l, i) => (
                    <li key={i} className="flex items-center justify-between gap-2 px-3 py-2">
                      <span>
                        {methodLabel(l.method)}
                        {l.reference && <span className="text-muted-foreground"> · {l.reference}</span>}
                        {l.tendered > l.amount && <span className="text-muted-foreground"> · change {formatCurrency(l.tendered - l.amount)}</span>}
                      </span>
                      <span className="flex items-center gap-2 tabular-nums">
                        {formatCurrency(l.amount)}
                        <button type="button" onClick={() => setLines((ls) => ls.filter((_, j) => j !== i))} aria-label={`Remove ${methodLabel(l.method)} payment`} className="rounded p-1 hover:bg-muted">
                          <X className="size-4" aria-hidden="true" />
                        </button>
                      </span>
                    </li>
                  ))}
                </ul>
              )}

              <div className="space-y-1.5">
                <label htmlFor="pay-entry" className="text-sm font-medium">
                  {cash ? 'Cash received' : `${methodLabel(type)} amount`}
                  {!cash && <span className="font-normal text-muted-foreground"> (empty: {formatCurrency(left)})</span>}
                </label>
                <Input
                  ref={entryRef}
                  id="pay-entry"
                  type="text"
                  inputMode="decimal"
                  autoComplete="off"
                  placeholder="0.00"
                  value={entry}
                  onChange={(e) => {
                    setError('');
                    setEntry(e.target.value.replace(/[^0-9.]/g, ''));
                  }}
                  className="h-12 text-right text-xl font-semibold tabular-nums"
                />
              </div>
              {WITH_REF[type] && (
                <Input aria-label={WITH_REF[type]} placeholder={`${WITH_REF[type]} (optional)`} value={reference} maxLength={100} onChange={(e) => setReference(e.target.value)} className="h-10" />
              )}
              {cash && (
                <>
                  <div className="grid grid-cols-4 gap-2" role="group" aria-label="Quick cash">
                    <button type="button" onClick={() => setEntry(left.toFixed(2))} className="min-h-10 rounded-md border bg-card text-sm font-medium hover:bg-muted">
                      Exact
                    </button>
                    {NOTES.map((n) => (
                      <button key={n} type="button" onClick={() => setEntry(String(n))} className="min-h-10 rounded-md border bg-card text-sm font-medium tabular-nums hover:bg-muted">
                        {formatCurrency(n)}
                      </button>
                    ))}
                  </div>
                  {/* Keypad for touch screens with room for it. */}
                  <div className="hidden grid-cols-3 gap-2 [@media(pointer:coarse)_and_(min-height:760px)]:grid" role="group" aria-label="Keypad">
                    {KEYS.map((k) => (
                      <button key={k} type="button" onClick={() => press(k)} aria-label={k === 'del' ? 'Delete last digit' : k} className="flex min-h-11 items-center justify-center rounded-md border bg-card text-lg font-semibold tabular-nums hover:bg-muted">
                        {k === 'del' ? <Delete className="size-5" aria-hidden="true" /> : k}
                      </button>
                    ))}
                  </div>
                  {entry !== '' && (
                    <div className={cn('flex items-center justify-between rounded-lg border px-4 py-2.5', change < 0 ? 'border-warning/40 bg-warning-bg' : 'bg-card')} aria-live="polite">
                      <span className="text-sm font-medium">{change < 0 ? 'Still to pay' : 'Change to give'}</span>
                      <span className="text-2xl font-bold tabular-nums">{formatCurrency(Math.abs(change))}</span>
                    </div>
                  )}
                </>
              )}
              <Button type="button" variant="outline" className="h-10 w-full" onClick={addPayment} disabled={left <= 0}>
                <Plus className="size-4" aria-hidden="true" /> Add this payment and take another
              </Button>
            </div>

            <div className="space-y-4">
              <fieldset>
                <legend className="mb-1.5 text-sm font-medium">Payment method</legend>
                <div className="grid grid-cols-3 gap-2" role="radiogroup" aria-label="Payment method">
                  {TYPES.map(({ value: v, label, icon: Icon }) => (
                    <button
                      key={v}
                      type="button"
                      role="radio"
                      aria-checked={type === v}
                      onClick={() => {
                        setType(v);
                        setEntry('');
                        setReference('');
                        setError('');
                      }}
                      className={cn(
                        'flex min-h-12 flex-col items-center justify-center gap-0.5 rounded-md border text-sm font-medium',
                        type === v ? 'border-primary bg-primary text-primary-foreground' : 'bg-card hover:bg-muted'
                      )}
                    >
                      <Icon className="size-4" aria-hidden="true" />
                      {label}
                    </button>
                  ))}
                </div>
              </fieldset>

              {onAccount ? (
                <div className="space-y-2 rounded-lg border bg-card px-3 py-2.5">
                  <label className="flex items-center gap-2 text-sm font-medium">
                    <input type="checkbox" checked={rest} onChange={(e) => setRest(e.target.checked)} className="size-4" />
                    Put what is left on {onAccount.name}&apos;s account
                  </label>
                  {rest && (
                    <div className="flex items-center gap-2 text-sm">
                      <label htmlFor="pay-due">Due by</label>
                      <Input id="pay-due" type="date" value={dueDate} min={todayInShop()} onChange={(e) => setDueDate(e.target.value)} className="h-9 w-44" />
                    </div>
                  )}
                  {onAccount.owes > 0 && (
                    <p className="text-xs text-muted-foreground">
                      Already owes {formatCurrency(onAccount.owes)}
                      {onAccount.limit != null ? ` of a ${formatCurrency(onAccount.limit)} limit` : ''}.
                    </p>
                  )}
                </div>
              ) : (
                <p className="text-xs text-muted-foreground">To sell on credit, choose a named customer. Selling on account needs the owner&apos;s permission.</p>
              )}

              <div className="space-y-1.5">
                <label htmlFor="pay-email" className="text-sm font-medium">
                  Email the receipt to <span className="font-normal text-muted-foreground">(optional)</span>
                </label>
                <Input id="pay-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="customer@example.com" autoComplete="off" className="h-11" />
              </div>

              {notesOpen ? (
                <div className="grid gap-3">
                  <div className="space-y-1.5">
                    <label htmlFor="pay-note" className="text-sm font-medium">Payment note</label>
                    <textarea id="pay-note" rows={2} maxLength={1000} value={paymentNote} onChange={(e) => setPaymentNote(e.target.value)} className="w-full rounded-md border border-input bg-card px-3 py-2 text-sm" />
                  </div>
                  <div className="space-y-1.5">
                    <label htmlFor="sale-note" className="text-sm font-medium">Sale note</label>
                    <textarea id="sale-note" rows={2} maxLength={1000} value={saleNote} onChange={(e) => setSaleNote(e.target.value)} className="w-full rounded-md border border-input bg-card px-3 py-2 text-sm" />
                  </div>
                </div>
              ) : (
                <button type="button" onClick={() => setNotesOpen(true)} className="inline-flex min-h-10 items-center gap-1.5 text-sm font-medium text-primary hover:underline">
                  <NotebookPen className="size-4" aria-hidden="true" /> Add note
                </button>
              )}
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" className="h-11 sm:min-w-28" onClick={onClose} disabled={busy}>
              Back to cart
            </Button>
            <Button type="submit" className="h-11 font-bold sm:min-w-56" disabled={busy}>
              {busy ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <CheckCircle2 className="size-4" aria-hidden="true" />}
              Complete sale · {formatCurrency(total)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
