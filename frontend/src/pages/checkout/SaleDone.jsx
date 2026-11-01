import { useEffect, useState } from 'react';
import { CheckCircle2, FileText, Plus, Printer } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { formatCurrency } from '@/lib/format';

const AUTO_KEY = 'pos.autoNewSale';
const AUTO_SECONDS = 5;

function readAuto() {
  try {
    return localStorage.getItem(AUTO_KEY) !== 'off';
  } catch {
    return true;
  }
}

// SaleDone: the sale is saved. Change due in large type, then Print,
// Receipt (view, PDF, email) and New sale. New sale can start by itself
// after 5 seconds (a per-browser choice); touching the screen pauses it.
export default function SaleDone({ sale, onPrint, onReceipt, onNewSale }) {
  const [auto, setAuto] = useState(readAuto);
  const [left, setLeft] = useState(AUTO_SECONDS);
  const [paused, setPaused] = useState(false);
  const open = !!sale;

  useEffect(() => {
    if (!open) return;
    setLeft(AUTO_SECONDS);
    setPaused(false);
  }, [open]);

  useEffect(() => {
    if (!open || !auto || paused) return;
    if (left <= 0) {
      onNewSale();
      return;
    }
    const t = setTimeout(() => setLeft((n) => n - 1), 1000);
    return () => clearTimeout(t);
  }, [open, auto, paused, left, onNewSale]);

  const toggleAuto = (on) => {
    setAuto(on);
    try {
      localStorage.setItem(AUTO_KEY, on ? 'on' : 'off');
    } catch {
      /* private window: not kept */
    }
  };

  const change = Number(sale?.change) || 0;
  const tendered = Number(sale?.tendered) || 0;

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onNewSale()}>
      {sale && (
        <DialogContent sheet className="sm:max-w-md" onPointerDown={() => setPaused(true)} onKeyDown={() => setPaused(true)}>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <CheckCircle2 className="size-6 text-success" aria-hidden="true" /> Sale complete
            </DialogTitle>
            <DialogDescription>
              {sale.reference} · {formatCurrency(sale.total)}
              {sale.payment_method ? ` · ${sale.payment_method}` : ''}
            </DialogDescription>
          </DialogHeader>

          {tendered > 0 ? (
            <div className="rounded-lg bg-accent px-4 py-4 text-center" aria-live="polite">
              <p className="text-sm font-medium text-accent-foreground">Change to give</p>
              <p className="text-5xl font-bold tabular-nums text-navy">{formatCurrency(change)}</p>
              <p className="mt-1 text-sm text-muted-foreground tabular-nums">Received {formatCurrency(tendered)}</p>
            </div>
          ) : (
            <p className="rounded-lg bg-accent px-4 py-4 text-center text-lg font-semibold text-navy">Paid {formatCurrency(sale.paid)}</p>
          )}
          {sale.receipt_email && <p className="text-center text-sm text-muted-foreground">The receipt will be emailed to {sale.receipt_email}.</p>}

          <div className="grid grid-cols-2 gap-2">
            <Button type="button" variant="outline" className="h-12" onClick={onPrint}>
              <Printer className="size-4" aria-hidden="true" /> Print
            </Button>
            <Button type="button" variant="outline" className="h-12" onClick={onReceipt}>
              <FileText className="size-4" aria-hidden="true" /> Receipt
            </Button>
          </div>

          <DialogFooter className="flex-col gap-2 sm:flex-col">
            <Button type="button" className="h-12 w-full text-base font-bold" onClick={onNewSale} autoFocus>
              <Plus className="size-5" aria-hidden="true" /> New sale
              {auto && !paused && <span className="font-normal tabular-nums"> in {left} s</span>}
            </Button>
            <label className="flex items-center justify-center gap-2 text-sm text-muted-foreground">
              <input type="checkbox" className="size-4 accent-[var(--primary)]" checked={auto} onChange={(e) => toggleAuto(e.target.checked)} />
              Start a new sale automatically
            </label>
          </DialogFooter>
        </DialogContent>
      )}
    </Dialog>
  );
}
