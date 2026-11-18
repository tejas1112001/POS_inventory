import { useEffect, useMemo, useState } from 'react';
import { Barcode, Loader2, Printer } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { variantLabel } from '@/components/pos/stock';
import { useScanner } from '@/hooks/useScanner';
import { beep, findByCode } from '@/lib/scan';
import { formatCurrency } from '@/lib/format';
import { api } from '@/services/api';
import { toast } from '../../../shared/toast/store';

const inputCls =
  'h-11 w-full rounded-md border border-input bg-background px-3 text-base outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 sm:h-9 sm:text-sm';

// Used when the layouts cannot be read (no settings:view).
const BUILTIN = [
  { id: 'a4-3x8', name: 'A4 sheet, 3 × 8 (65 × 34 mm)', columns: 3, rows: 8 },
  { id: 'roll-50x25', name: 'Label roll 50 × 25 mm', columns: 1, rows: 1 },
  { id: 'roll-38x25', name: 'Label roll 38 × 25 mm', columns: 1, rows: 1 },
];
const MAX_LABELS = 2000;
const LAYOUT_KEY = 'pos.labels.layout';

function savedLayout() {
  try {
    return localStorage.getItem(LAYOUT_KEY) || '';
  } catch {
    return '';
  }
}

// PrintLabelsDialog (industry P1 item 5, A9/A10): copies per variant, a
// label layout, in-store barcodes for variants without one, then the label
// PDF in a new tab. A scan adds one copy of the matching variant.
//
// From a product page it takes the product; from a stock count (item 7) it
// takes rows [{id, name, sku, itemBarcode, price?, copies}] and a title.
export default function PrintLabelsDialog({ product, rows, title, canGenerate, onClose, onChanged }) {
  const variants = rows || product.ProductVariants || [];
  const [copies, setCopies] = useState(() =>
    Object.fromEntries(variants.map((v) => [v.id, rows ? Math.min(500, Math.max(0, v.copies ?? 1)) : 1]))
  );
  const [layouts, setLayouts] = useState(BUILTIN);
  const [layoutId, setLayoutId] = useState(() => savedLayout() || BUILTIN[0].id);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState(null);

  useEffect(() => {
    api
      .get('/api/settings/labels', { quiet: true })
      .then((r) => {
        const all = [...(r.presets || []), ...(r.layouts || [])];
        if (all.length) setLayouts(all);
        setLayoutId((cur) => (all.some((l) => l.id === cur) ? cur : all[0]?.id || BUILTIN[0].id));
      })
      .catch(() => {});
  }, []);

  const total = Object.values(copies).reduce((n, c) => n + (Number(c) || 0), 0);
  const missing = variants.filter((v) => !v.itemBarcode);
  const layout = layouts.find((l) => l.id === layoutId);
  const sheets = layout && layout.columns * layout.rows > 1 ? Math.ceil(total / (layout.columns * layout.rows)) : 0;
  const products = useMemo(() => (rows ? [{ name: title, ProductVariants: rows }] : [product]), [product, rows, title]);
  const heading = rows ? title : product.name;
  const optionName = (v) => (rows ? v.name : variants.length > 1 ? variantLabel(v) : product.name);

  useScanner((code) => {
    const hit = findByCode(products, code);
    if (!hit) {
      beep();
      toast.error(rows ? `Nothing in this list has the code ${code}.` : `No option of ${product.name} has the code ${code}.`);
      return;
    }
    setCopies((cur) => ({ ...cur, [hit.variant.id]: Math.min((Number(cur[hit.variant.id]) || 0) + 1, 500) }));
  });

  const setCopy = (id, value) => {
    const n = value === '' ? '' : Math.max(0, Math.min(500, Math.floor(Number(value) || 0)));
    setCopies((cur) => ({ ...cur, [id]: n }));
  };

  const generate = async () => {
    setBusy('generate');
    setError(null);
    try {
      const r = await api.post('/api/barcodes/generate', { variantIds: missing.map((v) => v.id) }, { quiet: true });
      toast.success(`${r.generated.length} barcode${r.generated.length === 1 ? '' : 's'} created.`);
      onChanged?.();
    } catch (err) {
      setError(err);
    } finally {
      setBusy('');
    }
  };

  const print = async () => {
    setBusy('print');
    setError(null);
    try {
      localStorage.setItem(LAYOUT_KEY, layoutId);
    } catch {
      /* private mode */
    }
    try {
      const items = variants.filter((v) => Number(copies[v.id]) > 0).map((v) => ({ variantId: v.id, copies: Number(copies[v.id]) }));
      await api.openBlob('/api/labels/pdf', { method: 'POST', body: { layoutId, items }, quiet: true });
    } catch (err) {
      setError(err);
    } finally {
      setBusy('');
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent sheet className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Print labels for {heading}</DialogTitle>
          <DialogDescription>Set how many labels each option needs. Scanning a label adds one more of that option.</DialogDescription>
        </DialogHeader>
        <div className="grid max-h-[60vh] gap-4 overflow-y-auto">
          <label className="grid gap-1.5 text-sm font-medium">
            Label layout
            <select className={inputCls} value={layoutId} onChange={(e) => setLayoutId(e.target.value)}>
              {layouts.map((l) => (
                <option key={l.id} value={l.id}>
                  {l.name}
                </option>
              ))}
            </select>
          </label>
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase tracking-wide text-muted-foreground">
              <tr>
                <th scope="col" className="py-1 pr-3 font-medium">Option</th>
                <th scope="col" className="py-1 pr-3 font-medium">Barcode</th>
                <th scope="col" className="py-1 pr-3 text-right font-medium">Price</th>
                <th scope="col" className="w-24 py-1 font-medium">Copies</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {variants.map((v) => (
                <tr key={v.id}>
                  <td className="py-1.5 pr-3">
                    <div className="font-medium">{optionName(v)}</div>
                    <div className="text-xs text-muted-foreground">{v.sku}</div>
                  </td>
                  <td className="py-1.5 pr-3 font-mono text-xs">{v.itemBarcode || <span className="font-sans text-muted-foreground">None: prints the SKU</span>}</td>
                  <td className="py-1.5 pr-3 text-right tabular-nums">{v.price == null ? '—' : formatCurrency(v.price)}</td>
                  <td className="py-1.5">
                    <input
                      type="number"
                      min="0"
                      max="500"
                      className={inputCls}
                      value={copies[v.id]}
                      onChange={(e) => setCopy(v.id, e.target.value)}
                      aria-label={`Copies of ${optionName(v)}`}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {missing.length > 0 && canGenerate && (
            <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border p-3 text-sm">
              <span>
                {missing.length} option{missing.length === 1 ? ' has' : 's have'} no barcode. An in-store barcode (starting with 2) scans faster than a SKU.
              </span>
              <Button type="button" variant="outline" className="max-sm:h-11" disabled={!!busy} onClick={generate}>
                {busy === 'generate' ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <Barcode className="size-4" aria-hidden="true" />}
                Create barcodes
              </Button>
            </div>
          )}
          <p className="text-sm text-muted-foreground" role="status">
            {total} label{total === 1 ? '' : 's'}
            {sheets ? ` on ${sheets} sheet${sheets === 1 ? '' : 's'}` : ''}. In the print dialog choose <strong>Actual size</strong> (100 %), not
            &ldquo;Fit to page&rdquo;: shrunk barcodes may not scan.
          </p>
          {total > MAX_LABELS && <InlineAlert>At most {MAX_LABELS} labels per print.</InlineAlert>}
          {error && <InlineAlert>{error.message}</InlineAlert>}
        </div>
        <DialogFooter className="gap-2">
          <Button type="button" variant="outline" className="max-sm:h-11" onClick={onClose}>
            Close
          </Button>
          <Button type="button" className="max-sm:h-11" disabled={!!busy || total < 1 || total > MAX_LABELS} onClick={print}>
            {busy === 'print' ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <Printer className="size-4" aria-hidden="true" />}
            Print {total} label{total === 1 ? '' : 's'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
