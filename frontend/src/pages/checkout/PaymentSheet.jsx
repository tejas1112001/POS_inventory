import { useEffect, useRef, useState } from 'react';
import { Banknote, CheckCircle2, CreditCard, Delete, Landmark, Loader2, NotebookPen, ScrollText, Star } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { formatCurrency } from '@/lib/format';
import { cn } from '@/lib/utils';
import { parseAmount } from './useCheckout';

// Payment types: the values are what the server stores (unchanged).
const TYPES = [
  { value: 'cash', label: 'Cash', icon: Banknote },
  { value: 'credit', label: 'Card', icon: CreditCard },
  { value: 'cheque', label: 'Cheque', icon: ScrollText },
  { value: 'deposit', label: 'Deposit', icon: Landmark },
  { value: 'points', label: 'Points', icon: Star },
];
const NOTES = [10, 20, 50, 100, 200, 500, 2000];
const KEYS = ['1', '2', '3', '4', '5', '6', '7', '8', '9', '.', '0', 'del'];

// PaymentSheet: the amount (the server's total), payment type, cash
// received with quick notes and a keypad, change due, receipt email and
// notes. "Complete sale" calls onPay; if it fails the sheet stays open with
// the reason, and pressing it again reuses the same sale key (never charged
// twice). Two columns from 768 px, a bottom sheet on phones.
export default function PaymentSheet({ open, onClose, total, customerEmail = '', onPay }) {
  const [type, setType] = useState('cash');
  const [received, setReceived] = useState('');
  const [email, setEmail] = useState(customerEmail);
  const [notesOpen, setNotesOpen] = useState(false);
  const [paymentNote, setPaymentNote] = useState('');
  const [saleNote, setSaleNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const receivedRef = useRef(null);

  useEffect(() => {
    if (!open) return;
    setReceived('');
    setError('');
    setEmail(customerEmail);
  }, [open, customerEmail]);

  const cash = type === 'cash';
  const amount = parseAmount(received);
  const change = cash && received !== '' ? amount - total : 0;

  const press = (k) => {
    setError('');
    setReceived((r) => {
      if (k === 'del') return r.slice(0, -1);
      if (k === '.' && r.includes('.')) return r;
      if (/\.\d{2}$/.test(r)) return r;
      return `${r}${k}`;
    });
  };

  const submit = async (e) => {
    e.preventDefault();
    if (busy) return;
    if (cash && received.trim() === '') {
      setError('Enter the cash received (or press "Exact").');
      receivedRef.current?.focus();
      return;
    }
    if (cash && amount < total) {
      setError(`The cash received is ${formatCurrency(total - amount)} short of the total.`);
      return;
    }
    if (email.trim() && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) {
      setError('Enter a valid email address, or leave it empty.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      await onPay({ method: type, amountTendered: cash ? amount : 0, paymentNote, saleNote, receiptEmail: email.trim() });
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
          receivedRef.current?.focus();
        }}
      >
        <form onSubmit={submit} noValidate className="contents">
          <DialogHeader>
            <DialogTitle>Take payment</DialogTitle>
            <DialogDescription className="sr-only">Payment type, cash received, change and receipt email.</DialogDescription>
          </DialogHeader>

          {error && <InlineAlert>{error}</InlineAlert>}

          <div className="grid gap-5 md:grid-cols-2">
            <div className="space-y-3">
              <div className="rounded-lg bg-accent px-4 py-3">
                <p className="text-xs font-medium text-accent-foreground">Total to pay</p>
                <p className="text-3xl font-bold tabular-nums text-navy">{formatCurrency(total)}</p>
              </div>

              {cash ? (
                <>
                  <div className="space-y-1.5">
                    <label htmlFor="pay-received" className="text-sm font-medium">
                      Cash received
                    </label>
                    <Input
                      ref={receivedRef}
                      id="pay-received"
                      type="text"
                      inputMode="decimal"
                      autoComplete="off"
                      placeholder="0.00"
                      value={received}
                      onChange={(e) => {
                        setError('');
                        setReceived(e.target.value.replace(/[^0-9.]/g, ''));
                      }}
                      className="h-12 text-right text-xl font-semibold tabular-nums"
                    />
                  </div>
                  <div className="grid grid-cols-4 gap-2" role="group" aria-label="Quick cash">
                    <button type="button" onClick={() => setReceived(total.toFixed(2))} className="min-h-10 rounded-md border bg-card text-sm font-medium hover:bg-muted">
                      Exact
                    </button>
                    {NOTES.map((n) => (
                      <button key={n} type="button" onClick={() => setReceived(String(n))} className="min-h-10 rounded-md border bg-card text-sm font-medium tabular-nums hover:bg-muted">
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
                  <div className={cn('flex items-center justify-between rounded-lg border px-4 py-2.5', change < 0 ? 'border-warning/40 bg-warning-bg' : 'bg-card')} aria-live="polite">
                    <span className="text-sm font-medium">{change < 0 ? 'Still to pay' : 'Change to give'}</span>
                    <span className="text-2xl font-bold tabular-nums">{formatCurrency(Math.abs(change))}</span>
                  </div>
                </>
              ) : (
                <p className="rounded-lg border bg-card px-4 py-3 text-sm text-muted-foreground">
                  Take {formatCurrency(total)} by {TYPES.find((t) => t.value === type)?.label.toLowerCase()}. No change is given.
                </p>
              )}
            </div>

            <div className="space-y-4">
              <fieldset>
                <legend className="mb-1.5 text-sm font-medium">Payment type</legend>
                <div className="grid grid-cols-3 gap-2" role="radiogroup" aria-label="Payment type">
                  {TYPES.map(({ value, label, icon: Icon }) => (
                    <button
                      key={value}
                      type="button"
                      role="radio"
                      aria-checked={type === value}
                      onClick={() => {
                        setType(value);
                        setError('');
                      }}
                      className={cn(
                        'flex min-h-12 flex-col items-center justify-center gap-0.5 rounded-md border text-sm font-medium',
                        type === value ? 'border-primary bg-primary text-primary-foreground' : 'bg-card hover:bg-muted'
                      )}
                    >
                      <Icon className="size-4" aria-hidden="true" />
                      {label}
                    </button>
                  ))}
                </div>
              </fieldset>

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
