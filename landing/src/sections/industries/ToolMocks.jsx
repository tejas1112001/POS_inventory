import { useState } from 'react';
import { Ban, CircleCheck, PackagePlus, TriangleAlert } from 'lucide-react';
import { cn } from '@/lib/cn';
import { Button } from '@/components/ui/Button';
import { useHydrated } from '@/lib/useHydrated';

/* ---------------------------------------------------- size and colour */

const SIZES = ['S', 'M', 'L', 'XL', 'XXL'];
const COLOURS = [
  ['Indigo', 'bg-indigo-800', [3, 6, 0, 2, 1]],
  ['Maroon', 'bg-red-900', [2, 5, 4, 0, 1]],
  ['White', 'bg-white', [0, 3, 2, 2, 0]],
];

export function ClothingMatrix() {
  const [cell, setCell] = useState(['Maroon', 'L']);
  const [colour, size] = cell;
  const stock = COLOURS.find((c) => c[0] === colour)[2][SIZES.indexOf(size)];
  const sku = `K104-${colour.slice(0, 3).toUpperCase()}-${size}`;

  return (
    <div>
      <p className="text-lg font-extrabold text-ink">Cotton Kurta · K-104</p>
      <p className="text-[13px] text-muted">Stock by colour and size. Tap a box.</p>
      <div tabIndex={0} role="region" aria-label="Stock by colour and size" className="mt-4 overflow-x-auto">
        <table className="w-full min-w-[420px] border-separate border-spacing-2">
          <thead>
            <tr>
              <td className="w-[76px]" />
              {SIZES.map((s) => (
                <th key={s} scope="col" className="text-center text-[13px] font-bold text-muted">{s}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {COLOURS.map(([name, swatch, counts]) => (
              <tr key={name}>
                <th scope="row" className="text-left text-sm font-semibold text-ink">
                  <span className="flex items-center gap-2">
                    <span className={cn('size-3 rounded-full border border-slate-300', swatch)} aria-hidden="true" />
                    {name}
                  </span>
                </th>
                {counts.map((n, j) => {
                  const sel = name === colour && SIZES[j] === size;
                  return (
                    <td key={j} className="p-0">
                      <button
                        type="button"
                        aria-pressed={sel}
                        aria-label={`${name} ${SIZES[j]}: ${n} in stock`}
                        onClick={() => setCell([name, SIZES[j]])}
                        className={cn(
                          'h-12 w-full rounded-[10px] border text-base font-bold transition-colors duration-150',
                          sel
                            ? 'border-2 border-brand bg-brand font-extrabold text-white'
                            : n === 0
                              ? 'border-line bg-slate-100 text-slate-500'
                              : n <= 1
                                ? 'border-orange-300 bg-warn-soft text-ink'
                                : 'border-line bg-white text-ink hover:border-blue-300',
                        )}
                      >
                        {n}
                      </button>
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="mt-[18px] flex flex-wrap items-center justify-between gap-4 rounded-[14px] bg-surface p-4" aria-live="polite">
        <div>
          <p className="text-[15px] font-bold text-ink">{colour} / {size}</p>
          <p className="text-sm text-body">
            {stock === 0 ? 'Out of stock. Add to the next purchase.' : `${stock} in stock · ₹799 · ready to print a label`}
          </p>
        </div>
        <div className="rounded-lg border border-slate-300 bg-white px-3 py-2 text-center">
          <div className="barcode h-[34px] w-[150px]" aria-hidden="true" />
          <p className="mt-1 text-[11px] font-bold tracking-[0.06em] text-ink">{sku} · ₹799</p>
        </div>
      </div>
    </div>
  );
}

/* --------------------------------------------------------- size runs */

const SHOES = [4, 7, 9, 3, 5, 2];
const SHOE_MAX = 9;

export function FootwearRuns() {
  const hydrated = useHydrated(); // inline styles only in the browser (CSP)
  const [sel, setSel] = useState(3);
  const n = SHOES[sel];
  const low = n <= 3;
  return (
    <div>
      <p className="text-lg font-extrabold text-ink">Running Shoes · RS-220 · Black</p>
      <p className="text-[13px] text-muted">Pairs in stock by size. Tap a size.</p>
      <div className="mt-[18px] flex h-[180px] items-end gap-2.5">
        {SHOES.map((c, i) => {
          const on = sel === i;
          return (
            <button
              key={i}
              type="button"
              aria-pressed={on}
              aria-label={`UK ${i + 6}: ${c} pairs${c <= 3 ? ', low stock' : ''}`}
              onClick={() => setSel(i)}
              className="flex h-full flex-1 flex-col items-center justify-end gap-1.5"
            >
              <span className="text-sm font-extrabold text-ink">{c}</span>
              <span
                className={cn(
                  'w-full rounded-t-lg rounded-b-[3px] transition-colors',
                  c <= 3 ? 'bg-warn' : on ? 'bg-brand' : 'bg-blue-300',
                  on && 'outline-2 outline-offset-2 outline-ink',
                )}
                style={hydrated ? { height: `${Math.round((c / SHOE_MAX) * 100)}%` } : undefined}
              />
            </button>
          );
        })}
      </div>
      <div className="mt-2 flex gap-2.5">
        {SHOES.map((_, i) => (
          <span key={i} className="flex-1 text-center text-[13px] font-semibold text-muted">UK {i + 6}</span>
        ))}
      </div>
      <p className="mt-[18px] flex items-center gap-3 rounded-[14px] bg-surface p-4 text-[15px] text-ink" aria-live="polite">
        {low ? (
          <TriangleAlert className="size-[18px] shrink-0 text-warn" aria-hidden="true" />
        ) : (
          <CircleCheck className="size-[18px] shrink-0 text-emerald-500" aria-hidden="true" />
        )}
        <span>
          <strong>UK {sel + 6}</strong> · {low ? `${n} pairs left. Reorder suggested.` : `${n} pairs in stock.`}
        </span>
      </p>
    </div>
  );
}

/* --------------------------------------------------- batch and expiry */

export function PharmacyBatches() {
  const [sold, setSold] = useState(0);
  const b2104 = Math.max(0, 18 - sold);
  const fromNext = Math.max(0, sold - 18);
  const b2291 = Math.max(0, 40 - fromNext);
  const batches = [
    { id: 'B1980', exp: 'Oct 2026', qty: 6, status: 'Expired · blocked', cls: 'bg-red-100 text-red-800', icon: Ban },
    { id: 'B2104', exp: 'Jan 2027', qty: b2104, status: b2104 > 0 ? 'Sells first' : 'Sold out', cls: b2104 > 0 ? 'bg-blue-100 text-blue-800' : 'bg-slate-100 text-slate-600' },
    { id: 'B2291', exp: 'Aug 2027', qty: b2291, status: b2104 > 0 ? 'Waiting' : 'Sells first', cls: b2104 > 0 ? 'bg-slate-100 text-slate-600' : 'bg-blue-100 text-blue-800' },
  ];
  const msg =
    sold === 0
      ? 'Press "Sell 1 strip". MintoBill skips the expired batch and takes the earliest good one.'
      : `Sold ${sold} strip${sold > 1 ? 's' : ''}. Taken from ${fromNext > 0 ? 'B2104, then B2291' : 'B2104'}. B1980 is expired and was skipped.`;

  return (
    <div>
      <p className="text-lg font-extrabold text-ink">Paracetamol 500 mg · strip of 10</p>
      <p className="text-[13px] text-muted">Batches in stock, earliest expiry first</p>
      <div tabIndex={0} role="region" aria-label="Batches" className="mt-3.5 overflow-x-auto">
        <table className="w-full min-w-[420px] border-collapse text-sm">
          <thead>
            <tr className="text-left text-xs tracking-[0.06em] text-muted uppercase">
              <th scope="col" className="py-2">Batch</th>
              <th scope="col" className="py-2">Expiry</th>
              <th scope="col" className="py-2 text-right">Strips</th>
              <th scope="col" className="py-2 pl-3.5">Status</th>
            </tr>
          </thead>
          <tbody>
            {batches.map((b) => (
              <tr key={b.id} className="border-t border-line">
                <td className="py-3 font-bold text-ink">{b.id}</td>
                <td className="text-slate-700">{b.exp}</td>
                <td className="text-right font-bold text-ink tabular-nums">{b.qty}</td>
                <td className="pl-3.5">
                  <span className={cn('inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-bold', b.cls)}>
                    {b.icon && <b.icon className="size-3" aria-hidden="true" />}
                    {b.status}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="mt-4 flex flex-wrap gap-2.5">
        <Button onClick={() => setSold((s) => Math.min(58, s + 1))} disabled={sold >= 58} className="text-sm font-bold">
          Sell 1 strip
        </Button>
        <Button variant="outline" onClick={() => setSold(0)} className="text-sm">
          Reset
        </Button>
      </div>
      <p role="status" className="mt-3.5 rounded-[14px] bg-surface px-4 py-3.5 text-sm leading-relaxed text-ink">
        {msg}
      </p>
    </div>
  );
}

/* --------------------------------------------------- low-stock alerts */

const LOW = [
  { name: 'A4 notebook, 200 pages', min: 20, left: 6 },
  { name: 'Gel pen, blue', min: 50, left: 12 },
  { name: 'Steel water bottle 1 L', min: 10, left: 4 },
  { name: 'LED bulb 9 W', min: 15, left: 3 },
];

export function LowStockList() {
  const hydrated = useHydrated(); // inline styles only in the browser (CSP)
  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-lg font-extrabold text-ink">Low-stock alerts</p>
          <p className="text-[13px] text-muted">Items below their reorder level</p>
        </div>
        <span className="rounded-full bg-warn-soft px-3 py-1 text-[13px] font-bold text-warn-strong">{LOW.length} items</span>
      </div>
      <ul className="mt-3.5">
        {LOW.map((l) => (
          <li key={l.name} className="flex items-center gap-3.5 border-t border-line py-[13px]">
            <div className="min-w-0 flex-1">
              <p className="text-[15px] font-bold text-ink">{l.name}</p>
              <p className="text-[13px] text-muted">Reorder at {l.min}</p>
            </div>
            <div className="hidden h-2 w-[120px] overflow-hidden rounded-full bg-line sm:block" aria-hidden="true">
              <div className="h-2 bg-warn" style={hydrated ? { width: `${Math.round((l.left / l.min) * 100)}%` } : undefined} />
            </div>
            <p className="w-16 text-right font-extrabold text-ink">{l.left} left</p>
          </li>
        ))}
      </ul>
      <span className="mt-3.5 inline-flex min-h-11 items-center gap-2 rounded-[10px] bg-brand px-4 text-sm font-bold text-white">
        <PackagePlus className="size-4" aria-hidden="true" />
        Create purchase order
      </span>
    </div>
  );
}
