import { ShoppingCart } from 'lucide-react';
import IconTile from './IconTile';

// CartHeader: "Current sale" with the item count (both checkout screens).
// children: one line under the title (the customer), so the cart needs no
// separate customer bar.
export default function CartHeader({ count, title = 'Current sale', children }) {
  return (
    <div className="flex min-w-0 items-center gap-2.5">
      <IconTile icon={ShoppingCart} size="sm" />
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <h2 className="text-base leading-tight font-bold">{title}</h2>
          {count > 0 && <span className="rounded-full bg-primary px-2 py-0.5 text-xs font-bold text-primary-foreground tabular-nums">{count}</span>}
        </div>
        {children && <div className="flex min-w-0 items-center text-xs text-muted-foreground">{children}</div>}
      </div>
    </div>
  );
}
