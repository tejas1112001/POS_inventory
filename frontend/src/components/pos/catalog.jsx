// Shared pieces of the checkout screens (old POS and the new checkout):
// stock helpers, product tiles, category pills and the "Choose option" sheet.
import { Lock, Star } from 'lucide-react';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { ProductImage } from '@/components/shared/product-image';
import { formatCurrency } from '@/lib/format';
import { cn } from '@/lib/utils';
import { productStock, variantLabel, variantStock, variantsOf } from './stock';

const LOW = 5; // "3 left" from this number down

export function StockBadge({ qty }) {
  if (qty <= 0) return <span className="rounded bg-danger-bg px-1.5 py-0.5 text-[11px] font-medium text-danger-fg">Out of stock</span>;
  if (qty <= LOW) return <span className="rounded bg-warning-bg px-1.5 py-0.5 text-[11px] font-medium text-warning-fg">{qty} left</span>;
  return null;
}

export function CategoryPill({ active, onClick, children }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        'inline-flex min-h-9 shrink-0 items-center gap-1 rounded-full px-4 text-sm font-medium transition-colors',
        active ? 'bg-primary text-primary-foreground' : 'bg-card text-foreground ring-1 ring-border hover:bg-muted'
      )}
    >
      {children}
    </button>
  );
}

// ProductTile: picture, name (two lines), category, price, stock badge and
// "N options". Optional: a favourite star and the count already in the cart.
export function ProductTile({ product, lockedByOther: locked, onPick, inCart = 0, favourite, onToggleFavourite }) {
  const variants = variantsOf(product);
  const stock = productStock(product);
  const out = stock <= 0;
  const disabled = locked || out;
  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => !disabled && onPick(product)}
        disabled={disabled}
        aria-label={`${product.name}, ${formatCurrency(product.price)}${variants.length > 1 ? `, ${variants.length} options` : ''}${out ? ', out of stock' : ''}${locked ? ', in use at another till' : ''}${inCart ? `, ${inCart} in cart` : ''}`}
        className="group flex h-full w-full flex-col overflow-hidden rounded-lg border bg-card text-left shadow-xs transition hover:border-primary/40 hover:shadow-sm focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-not-allowed disabled:opacity-55"
      >
        <div className="relative aspect-[4/3] w-full bg-muted">
          <ProductImage name={product.name} src={product.image} className="!h-full !w-full rounded-none text-lg" size={0} />
          {variants.length > 1 && (
            <span className="absolute top-1.5 left-1.5 rounded bg-white/95 px-1.5 py-0.5 text-[11px] font-medium text-slate-700 shadow-xs">
              {variants.length} options
            </span>
          )}
          {inCart > 0 && (
            <span className="absolute right-1.5 bottom-1.5 rounded-full bg-primary px-2 py-0.5 text-[11px] font-semibold text-primary-foreground tabular-nums shadow-xs">
              {inCart} in cart
            </span>
          )}
          {locked && (
            <span className="absolute inset-0 flex items-center justify-center bg-white/70">
              <span className="inline-flex items-center gap-1 rounded bg-slate-800 px-2 py-1 text-xs font-medium text-white">
                <Lock className="size-3" aria-hidden="true" /> In use
              </span>
            </span>
          )}
        </div>
        <div className="flex flex-1 flex-col gap-1 p-2">
          <p className="line-clamp-2 min-h-[2.5em] text-sm leading-tight font-medium text-foreground">{product.name}</p>
          {product.category && <p className="truncate text-xs text-muted-foreground">{product.category}</p>}
          <div className="mt-auto flex items-center justify-between gap-1 pt-1">
            <span className="font-semibold tabular-nums text-foreground">{formatCurrency(product.price)}</span>
            <StockBadge qty={stock} />
          </div>
        </div>
      </button>
      {onToggleFavourite && (
        <button
          type="button"
          onClick={() => onToggleFavourite(product.id)}
          aria-pressed={!!favourite}
          aria-label={favourite ? `Remove ${product.name} from favourites` : `Add ${product.name} to favourites`}
          className="absolute top-1 right-1 flex size-9 items-center justify-center rounded-full bg-white/90 text-slate-500 shadow-xs hover:text-amber-500 sm:size-8"
        >
          <Star className={cn('size-4', favourite && 'fill-amber-400 text-amber-500')} aria-hidden="true" />
        </button>
      )}
    </div>
  );
}

// VariantSheet: "Choose option" for a product with several variants.
export function VariantSheet({ product, lines, onPick, onClose }) {
  return (
    <Dialog open={!!product} onOpenChange={(o) => !o && onClose()}>
      {product && (
        <DialogContent sheet className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Choose option</DialogTitle>
            <DialogDescription>{product.name}</DialogDescription>
          </DialogHeader>
          <ul className="grid gap-2" aria-label="Options">
            {variantsOf(product).map((v) => {
              const stock = variantStock(v);
              const inCart = lines.find((l) => l.variantId === v.id)?.quantity || 0;
              const full = stock <= 0 || inCart >= stock;
              return (
                <li key={v.id}>
                  <button
                    type="button"
                    disabled={full}
                    onClick={() => {
                      onPick(product, v);
                      onClose();
                    }}
                    className="flex min-h-14 w-full items-center justify-between gap-3 rounded-lg border bg-card px-3 py-2 text-left hover:border-primary hover:bg-accent disabled:cursor-not-allowed disabled:opacity-55"
                  >
                    <span className="min-w-0">
                      <span className="block truncate font-medium">{variantLabel(v)}</span>
                      <span className="block truncate text-xs text-muted-foreground">
                        {v.sku}
                        {inCart ? ` · ${inCart} in cart` : ''}
                      </span>
                    </span>
                    <span className="flex shrink-0 flex-col items-end gap-1">
                      <span className="font-semibold tabular-nums">{formatCurrency(v.price)}</span>
                      {stock <= 0 ? <StockBadge qty={0} /> : <span className="text-xs text-muted-foreground tabular-nums">{stock} in stock</span>}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        </DialogContent>
      )}
    </Dialog>
  );
}
