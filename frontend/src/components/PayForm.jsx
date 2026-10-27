import { useState, useEffect } from 'react';
import { X, CheckCircle2, Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { formatCurrency } from '@/lib/format';
import { cn } from '@/lib/utils';

export default function PayForm({ isOpen, onClose, total, onPaySubmit, customerEmail = '' }) {
  const [formData, setFormData] = useState({
    receivedAmount: '',
    payingAmount: total.toFixed(2),
    change: '0.00',
    paymentType: 'cash',
    paymentNote: '',
    saleNote: '',
    quickCash: '10',
  });
  const [receiptEmail, setReceiptEmail] = useState(customerEmail);
  const [submitting, setSubmitting] = useState(false);

  const [showQuickCash, setShowQuickCash] = useState(true);

  useEffect(() => {
    if (!isOpen) return;
    setFormData((prev) => ({
      ...prev,
      payingAmount: total.toFixed(2),
      change: '0.00',
      receivedAmount: '',
    }));
  }, [isOpen, total]);

  const handleChange = (e) => {
    const { name, value } = e.target;
    setFormData((prev) => {
      const next = { ...prev, [name]: value };
      if (name === 'receivedAmount') {
        const received = parseFloat(value) || 0;
        const paying = parseFloat(prev.payingAmount) || 0;
        const rawDiff = received - paying;
        next.change = (rawDiff > 0 ? rawDiff : 0).toFixed(2);
      }
      return next;
    });
  };

  const handlePaymentTypeChange = (e) => {
    const type = e.target.value;
    setFormData((prev) => ({ ...prev, paymentType: type }));
    setShowQuickCash(type === 'cash');
    if (type !== 'cash') {
      setFormData((prev) => ({ ...prev, change: '0.00' }));
    }
  };

  const handleQuickCashSelect = (amount) => {
    setFormData((prev) => ({
      ...prev,
      quickCash: amount,
      receivedAmount: amount,
      change: (parseFloat(amount) - parseFloat(prev.payingAmount)).toFixed(2),
    }));
  };

  // The POS shows any error after the form closes; the button stays
  // disabled while the sale is being saved, so it cannot be sent twice.
  const handleSubmit = async (e) => {
    e.preventDefault();
    if (submitting) return;
    setSubmitting(true);
    try {
      await onPaySubmit({
        method: formData.paymentType,
        amountTendered: parseFloat(formData.receivedAmount) || 0,
        paymentNote: formData.paymentNote,
        saleNote: formData.saleNote,
        receiptEmail: receiptEmail.trim(),
      });
    } finally {
      setSubmitting(false);
      onClose();
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Finalize Sale</DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="rounded-lg bg-primary/5 p-4 text-center">
            <p className="text-xs text-muted-foreground">Total Amount</p>
            <p className="text-3xl font-bold text-primary">{formatCurrency(total)}</p>
          </div>

          <div className="grid grid-cols-3 gap-3">
            <div className="space-y-1.5">
              <Label className="text-xs">Received</Label>
              <Input
                type="number"
                name="receivedAmount"
                value={formData.receivedAmount}
                onChange={handleChange}
                min="0"
                step="0.01"
                required
                autoFocus
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">Paying</Label>
              <Input type="text" value={formData.payingAmount} readOnly />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">Change</Label>
              <Input type="text" value={formData.change} readOnly />
            </div>
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs">Payment Type</Label>
            <select
              name="paymentType"
              value={formData.paymentType}
              onChange={handlePaymentTypeChange}
              className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]"
            >
              <option value="credit">Credit Card</option>
              <option value="cash">Cash</option>
              <option value="cheque">Cheque</option>
              <option value="deposit">Deposit</option>
              <option value="points">Points</option>
            </select>
          </div>

          {showQuickCash && (
            <div className="space-y-2">
              <Label className="text-xs">Quick Cash</Label>
              <div className="grid grid-cols-3 gap-2">
                {[10, 50, 100, 500, 1000, 5000].map((amount) => (
                  <button
                    key={amount}
                    type="button"
                    onClick={() => handleQuickCashSelect(String(amount))}
                    className={cn(
                      'rounded-md border py-2 text-sm font-medium transition-colors',
                      formData.quickCash === String(amount)
                        ? 'border-primary bg-primary text-primary-foreground'
                        : 'bg-card hover:bg-accent'
                    )}
                  >
                    {formatCurrency(amount)}
                  </button>
                ))}
              </div>
            </div>
          )}

          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label className="text-xs">Payment Note</Label>
              <textarea
                name="paymentNote"
                value={formData.paymentNote}
                onChange={handleChange}
                className="flex min-h-[60px] w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]"
                rows="2"
                placeholder="Add payment note"
              />
            </div>
            <div className="space-y-1.5">
              <Label className="text-xs">Sale Note</Label>
              <textarea
                name="saleNote"
                value={formData.saleNote}
                onChange={handleChange}
                className="flex min-h-[60px] w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]"
                rows="2"
                placeholder="Add sale note"
              />
            </div>
          </div>

          <div className="space-y-1.5">
            <Label className="text-xs" htmlFor="receipt-email">Email the receipt to (optional)</Label>
            <Input
              id="receipt-email"
              type="email"
              value={receiptEmail}
              onChange={(e) => setReceiptEmail(e.target.value)}
              placeholder="customer@example.com"
              autoComplete="off"
            />
          </div>

          <div className="flex gap-2 pt-2">
            <Button type="button" variant="outline" className="flex-1" onClick={onClose}>
              <X className="size-4" /> Cancel
            </Button>
            <Button type="submit" className="flex-1 h-11 font-bold" disabled={submitting}>
              {submitting ? <Loader2 className="size-4 animate-spin" /> : <CheckCircle2 className="size-4" />} Process Payment
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
