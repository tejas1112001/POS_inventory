import { useEffect, useMemo, useRef, useState } from 'react';
import { Banknote, CheckCircle2, CreditCard, Landmark, Loader2, NotebookPen, ScrollText, Star } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { formatCurrency } from '@/lib/format';
import { cn } from '@/lib/utils';

// Payment types: the values are what the server stores (unchanged).
const TYPES = [
  { value: 'cash', label: 'Cash', icon: Banknote },
  { value: 'credit', label: 'Card', icon: CreditCard },
  { value: 'cheque', label: 'Cheque', icon: ScrollText },
  { value: 'deposit', label: 'Deposit', icon: Landmark },
  { value: 'points', label: 'Points', icon: Star },
];
const QUICK = [10, 50, 100, 500, 1000, 5000];

const parseAmount = (v) => {
  const n = parseFloat(String(v).replace(/[^0-9.]/g, ''));
  return Number.isFinite(n) ? n : 0;
};

// PayForm: "Finalize sale". Two columns from 768 px, a bottom sheet on
// phones; the footer (Cancel / Process Payment) stays visible.
export default function PayForm({ isOpen, onClose, total, onPaySubmit, customerEmail = '' }) {
  const [type, setType] = useState('cash');
  const [received, setReceived] = useState('');
  const [receiptEmail, setReceiptEmail] = useState(customerEmail);
  const [notesOpen, setNotesOpen] = useState(false);
  const [paymentNote, setPaymentNote] = useState('');
  const [saleNote, setSaleNote] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [problem, setProblem] = useState('');
  const receivedRef = useRef(null);

  useEffect(() => {
    if (!isOpen) return;
    setReceived('');
    setProblem('');
  }, [isOpen, total]);

  const cash = type === 'cash';
  const amount = parseAmount(received);
  const change = cash && received !== '' ? amount - total : 0;
  const totalText = useMemo(() => formatCurrency(total), [total]);

  // The POS shows any error after the form closes; the button stays
  // disabled while the sale is being saved, so it cannot be sent twice.
  const handleSubmit = async (e) => {
    e.preventDefault();
    if (submitting) return;
    if (cash && received.trim() === '') {
      setProblem('Enter the cash received (use "Exact" for the total).');
      receivedRef.current?.focus();
      return;
    }
    if (receiptEmail.trim() && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(receiptEmail.trim())) {
      setProblem('Enter a valid email address, or leave it empty.');
      return;
    }
    setSubmitting(true);
    try {
      await onPaySubmit({
        method: type,
        amountTendered: cash ? amount : 0,
        paymentNote,
        saleNote,
        receiptEmail: receiptEmail.trim(),
      });
    } finally {
      setSubmitting(false);
      onClose();
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && !submitting && onClose()}>
      <DialogContent sheet className="sm:max-w-3xl" onOpenAutoFocus={(e) => { e.preventDefault(); receivedRef.current?.focus(); }}>
        <form onSubmit={handleSubmit} noValidate className="contents">
          <DialogHeader>
            <DialogTitle>Finalize sale</DialogTitle>
            <DialogDescription className="sr-only">Payment type, amount received and receipt email.</DialogDescription>
          </DialogHeader>

          <div className="grid gap-5 md:grid-cols-2">
            {/* Left: amount */}
            <div className="space-y-4">
              <div className="rounded-lg bg-accent px-4 py-3">
                <p className="text-xs font-medium text-accent-foreground">Total to pay</p>
                <p className="text-3xl font-bold tabular-nums text-navy">{totalText}</p>
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
                      name="receivedAmount"
                      type="text"
                      inputMode="decimal"
                      autoComplete="off"
                      placeholder="0.00"
                      value={received}
                      onChange={(e) => {
                        setProblem('');
                        setReceived(e.target.value.replace(/[^0-9.]/g, ''));
                      }}
                      className="h-12 text-right text-xl font-semibold tabular-nums"
                    />
                  </div>
                  <div className="grid grid-cols-4 gap-2" role="group" aria-label="Quick cash">
                    <button type="button" onClick={() => setReceived(String(total.toFixed(2)))} className="col-span-1 min-h-10 rounded-md border bg-card text-sm font-medium hover:bg-muted">
                      Exact
                    </button>
                    {QUICK.slice(0, 3).map((n) => (
                      <button key={n} type="button" onClick={() => setReceived(String(n))} className="min-h-10 rounded-md border bg-card text-sm font-medium tabular-nums hover:bg-muted">
                        {formatCurrency(n)}
                      </button>
                    ))}
                    {QUICK.slice(3).map((n) => (
                      <button key={n} type="button" onClick={() => setReceived(String(n))} className="col-span-1 min-h-10 rounded-md border bg-card text-sm font-medium tabular-nums hover:bg-muted">
                        {formatCurrency(n)}
                      </button>
                    ))}
                  </div>
                  <div
                    className={cn('flex items-center justify-between rounded-lg border px-4 py-2.5', change < 0 ? 'border-warning/40 bg-warning-bg' : 'bg-card')}
                    aria-live="polite"
                  >
                    <span className="text-sm font-medium">{change < 0 ? 'Still to pay' : 'Change to give'}</span>
                    <span className="text-xl font-bold tabular-nums">{formatCurrency(Math.abs(change))}</span>
                  </div>
                </>
              ) : (
                <p className="rounded-lg border bg-card px-4 py-3 text-sm text-muted-foreground">
                  Take {totalText} by {TYPES.find((t) => t.value === type)?.label.toLowerCase()}. No change is given.
                </p>
              )}
            </div>

            {/* Right: type, email, notes */}
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
                        setProblem('');
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
                <label htmlFor="receipt-email" className="text-sm font-medium">
                  Email the receipt to <span className="font-normal text-muted-foreground">(optional)</span>
                </label>
                <Input
                  id="receipt-email"
                  type="email"
                  value={receiptEmail}
                  onChange={(e) => setReceiptEmail(e.target.value)}
                  placeholder="customer@example.com"
                  autoComplete="off"
                  className="h-11"
                />
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

          {problem && (
            <p role="alert" className="rounded-md bg-danger-bg px-3 py-2 text-sm text-danger-fg">
              {problem}
            </p>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" className="h-11 sm:min-w-28" onClick={onClose} disabled={submitting}>
              Cancel
            </Button>
            <Button type="submit" className="h-11 font-bold sm:min-w-48" disabled={submitting}>
              {submitting ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <CheckCircle2 className="size-4" aria-hidden="true" />}
              Process Payment
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
