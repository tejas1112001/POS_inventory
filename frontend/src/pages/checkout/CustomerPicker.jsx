import { useMemo, useState } from 'react';
import { Check, Search, UserPlus } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import CustomerForm from '@/components/CustomerForm';
import { cn } from '@/lib/utils';

// CustomerPicker (F4): search by name, phone or email; Enter picks the
// first match. "Add customer" opens the customer form.
export default function CustomerPicker({ open, onClose, customers, value, onPick, onAdded }) {
  const [q, setQ] = useState('');
  const [adding, setAdding] = useState(false);
  const shown = useMemo(() => {
    const s = q.trim().toLowerCase();
    const list = s ? customers.filter((c) => [c.name, c.phone, c.email].some((x) => x && String(x).toLowerCase().includes(s))) : customers;
    return list.slice(0, 50);
  }, [customers, q]);

  const pick = (c) => {
    onPick(c.id);
    setQ('');
    onClose();
  };

  return (
    <>
      <Dialog open={open && !adding} onOpenChange={(o) => !o && onClose()}>
        <DialogContent sheet className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Customer</DialogTitle>
            <DialogDescription>Search by name, phone or email.</DialogDescription>
          </DialogHeader>
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
            <Input
              autoFocus
              type="search"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && shown[0]) {
                  e.preventDefault();
                  pick(shown[0]);
                }
              }}
              placeholder="Search customers…"
              aria-label="Search customers"
              className="h-11 pl-9"
            />
          </div>
          <ul className="grid max-h-[50dvh] gap-1 overflow-y-auto" aria-label="Customers">
            {shown.map((c) => (
              <li key={c.id}>
                <button
                  type="button"
                  onClick={() => pick(c)}
                  className={cn('flex min-h-12 w-full items-center justify-between gap-3 rounded-md px-3 py-2 text-left hover:bg-muted', String(c.id) === String(value) && 'bg-accent')}
                >
                  <span className="min-w-0">
                    <span className="block truncate font-medium">{c.name}</span>
                    <span className="block truncate text-xs text-muted-foreground">{[c.phone, c.email].filter(Boolean).join(' · ') || '—'}</span>
                  </span>
                  {String(c.id) === String(value) && <Check className="size-4 shrink-0 text-primary" aria-label="Selected" />}
                </button>
              </li>
            ))}
            {!shown.length && <li className="px-3 py-6 text-center text-sm text-muted-foreground">No customer matches.</li>}
          </ul>
          <Button type="button" variant="outline" className="h-11" onClick={() => setAdding(true)}>
            <UserPlus className="size-4" aria-hidden="true" /> Add customer
          </Button>
        </DialogContent>
      </Dialog>
      {adding && (
        <CustomerForm
          isOpen={adding}
          onClose={() => setAdding(false)}
          onAddCustomer={(c) => {
            onAdded(c);
            setAdding(false);
            pick(c);
          }}
        />
      )}
    </>
  );
}
