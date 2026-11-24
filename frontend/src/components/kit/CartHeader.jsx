import { ShoppingCart } from 'lucide-react';
import IconTile from './IconTile';

// CartHeader: "Current sale" with the item count (both checkout screens).
export default function CartHeader({ count, title = 'Current sale' }) {
  return (
    <div className="flex items-center gap-2">
      <IconTile icon={ShoppingCart} size="sm" />
      <h2 className="text-base font-bold">{title}</h2>
      {count > 0 && <span className="rounded-full bg-primary px-2 py-0.5 text-xs font-bold text-primary-foreground tabular-nums">{count}</span>}
    </div>
  );
}
