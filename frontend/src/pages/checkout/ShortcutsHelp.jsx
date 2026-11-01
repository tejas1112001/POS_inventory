import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';

const SHORTCUTS = [
  ['F2', 'Search products'],
  ['Enter', 'Add the exact SKU, or the only match'],
  ['F4', 'Choose the customer'],
  ['F8', 'Discount'],
  ['F10 or Ctrl+Enter', 'Pay'],
  ['↑ / ↓', 'Select a cart line (search box empty)'],
  ['+ / −', 'One more / one less of the selected line'],
  ['Delete', 'Remove the selected line'],
  ['Esc', 'Clear the search, or close a window'],
  ['?', 'This list'],
];

// ShortcutsHelp lists the checkout's keyboard shortcuts.
export default function ShortcutsHelp({ open, onClose }) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Keyboard shortcuts</DialogTitle>
          <DialogDescription>They work while no window is open.</DialogDescription>
        </DialogHeader>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          {SHORTCUTS.map(([k, what]) => (
            <div key={k} className="contents">
              <dt>
                <kbd className="rounded border bg-muted px-1.5 py-0.5 font-mono text-xs">{k}</kbd>
              </dt>
              <dd className="text-muted-foreground">{what}</dd>
            </div>
          ))}
        </dl>
      </DialogContent>
    </Dialog>
  );
}
