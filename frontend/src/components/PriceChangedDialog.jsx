import { AlertTriangle } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { formatCurrency } from '@/lib/format';

// PriceChangedDialog explains a 409 PRICE_CHANGED: the server charges
// catalog prices only, and some lines no longer match. onAccept updates
// the cart to the current prices; nothing was sold yet.
export default function PriceChangedDialog({ changes, nameOf, onAccept, onClose }) {
  return (
    <Dialog open={!!changes} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <AlertTriangle className="size-5 text-amber-600" /> Prices have changed
          </DialogTitle>
        </DialogHeader>
        <p className="text-sm text-muted-foreground">
          Nothing was charged. These prices changed since the items were added. Use the current prices and check the total
          with the customer before taking payment again.
        </p>
        <ul className="divide-y rounded-md border text-sm">
          {(changes || []).map((c) => (
            <li key={`${c.variantId}-${c.index}`} className="flex items-center justify-between gap-3 px-3 py-2">
              <span className="min-w-0 truncate">{nameOf?.(c) || c.sku}</span>
              <span className="shrink-0 whitespace-nowrap">
                <span className="text-muted-foreground line-through">{formatCurrency(c.sent)}</span>{' '}
                <span className="font-semibold">{formatCurrency(c.unitPrice)}</span>
              </span>
            </li>
          ))}
        </ul>
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={onAccept}>Use current prices</Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
