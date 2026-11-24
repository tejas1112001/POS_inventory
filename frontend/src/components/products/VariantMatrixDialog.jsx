import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { api } from '@/services/api';
import { useModules } from '@/hooks/useModules';
import { useShopSettings } from '@/hooks/useShopSettings';
import { formatCurrency } from '@/lib/format';
import { mrpWarning } from '@/lib/mrp';
import { toast } from '../../../shared/toast/store';

const inputCls =
  'h-11 w-full rounded-lg border border-input bg-card px-3 text-base outline-none focus-visible:border-primary focus-visible:ring-[3px] focus-visible:ring-ring/25 sm:h-9 sm:text-sm';

// VariantMatrixDialog (industry P1 item 4, A8): pick the values of each
// variant dimension, preview every combination (existing ones skipped),
// then create them with stock 0. SKUs come from a pattern.
export default function VariantMatrixDialog({ product, defs, onClose, onCreated }) {
  const dims = product.variantDims || [];
  const first = (product.ProductVariants || [])[0];
  const [picked, setPicked] = useState(() => Object.fromEntries(dims.map((k) => [k, []])));
  const [texts, setTexts] = useState(() => Object.fromEntries(dims.map((k) => [k, ''])));
  const [pattern, setPattern] = useState('');
  const [price, setPrice] = useState(first ? String(first.price) : '');
  const [cost, setCost] = useState(first ? String(first.cost) : '');
  const [mrp, setMrp] = useState(first?.mrp != null ? String(first.mrp) : '');
  const { settings } = useShopSettings();
  const mrpWarn = mrpWarning({ price, mrp, taxRate: product.tax, taxType: product.taxType, pricesIncludeTax: settings?.pricesIncludeTax }, formatCurrency);
  const [preview, setPreview] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const { hasModule } = useModules();
  const canBarcode = hasModule('barcode_labels');
  const [barcodes, setBarcodes] = useState(false);

  const defaultPattern = (product.articleNo ? '{article}' : 'P{id}') + dims.map((k) => `-{${k}}`).join('');
  const valuesOf = (k) => (defs[k]?.kind === 'choice' ? picked[k] : texts[k].split(',').map((s) => s.trim()).filter(Boolean));
  const body = () => ({
    dims: Object.fromEntries(dims.map((k) => [k, valuesOf(k)])),
    skuPattern: pattern.trim() || undefined,
    price: price === '' ? undefined : Number(price),
    cost: cost === '' ? undefined : Number(cost),
    mrp: mrp === '' ? undefined : Number(mrp),
    generateBarcodes: canBarcode && barcodes ? true : undefined,
  });
  const toggle = (k, c) => {
    setPreview(null);
    setPicked((cur) => ({ ...cur, [k]: cur[k].includes(c) ? cur[k].filter((x) => x !== c) : [...cur[k], c] }));
  };

  const run = async (dry) => {
    setBusy(true);
    setError(null);
    try {
      const r = await api.post(`/api/products/${product.id}/variants/matrix${dry ? '?dryRun=true' : ''}`, body(), { quiet: true });
      if (dry) setPreview(r);
      else {
        toast.success(`${r.created} variants created with stock 0${barcodes && canBarcode ? ', each with an in-store barcode' : ''}.`);
        onCreated();
        onClose();
      }
    } catch (err) {
      setError(err);
      setPreview(null);
    } finally {
      setBusy(false);
    }
  };

  const collisions = error?.details?.collisions;
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent sheet className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Create variants of {product.name}</DialogTitle>
          <DialogDescription>Every combination of the values you pick becomes a variant with stock 0. Variants the product already has are skipped.</DialogDescription>
        </DialogHeader>
        <div className="grid max-h-[60vh] gap-4 overflow-y-auto">
          {dims.map((k) => {
            const d = defs[k];
            if (!d) return null;
            return (
              <fieldset key={k}>
                <legend className="mb-1 text-sm font-medium">{d.label}</legend>
                {d.kind === 'choice' ? (
                  <div className="flex flex-wrap gap-2">
                    {d.choices.map((c) => (
                      <label key={c} className="flex items-center gap-1.5 rounded-lg border px-2 py-1 text-sm">
                        <input type="checkbox" className="size-4" checked={picked[k].includes(c)} onChange={() => toggle(k, c)} />
                        {c}
                      </label>
                    ))}
                  </div>
                ) : (
                  <input
                    className={inputCls}
                    value={texts[k]}
                    placeholder="Separate values with commas, e.g. Red, Navy blue, White"
                    onChange={(e) => {
                      setPreview(null);
                      setTexts((cur) => ({ ...cur, [k]: e.target.value }));
                    }}
                    aria-label={`${d.label} values`}
                  />
                )}
              </fieldset>
            );
          })}
          <div className="grid gap-3 sm:grid-cols-3">
            <label className="grid gap-1.5 text-sm font-medium sm:col-span-3">
              SKU pattern
              <input className={inputCls} value={pattern} placeholder={defaultPattern} onChange={(e) => { setPreview(null); setPattern(e.target.value); }} maxLength={120} />
              <span className="text-xs font-normal text-muted-foreground">
                Tokens: {'{article}'} (article number), {'{id}'} (product number){dims.map((k) => `, {${k}}`).join('')}. Values are upper-cased without spaces.
              </span>
            </label>
            <label className="grid gap-1.5 text-sm font-medium">
              Price
              <input type="number" min="0" step="0.01" className={inputCls} value={price} onChange={(e) => setPrice(e.target.value)} />
            </label>
            <label className="grid gap-1.5 text-sm font-medium">
              Cost
              <input type="number" min="0" step="0.01" className={inputCls} value={cost} onChange={(e) => setCost(e.target.value)} />
            </label>
            <label className="grid gap-1.5 text-sm font-medium">
              MRP
              <input type="number" min="0" step="0.01" className={inputCls} value={mrp} placeholder="Optional" onChange={(e) => setMrp(e.target.value)} />
            </label>
            {mrpWarn && <p className="text-xs text-warning-fg sm:col-span-3">{mrpWarn}</p>}
          </div>
          {canBarcode && (
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" className="size-4" checked={barcodes} onChange={(e) => setBarcodes(e.target.checked)} />
              Give each new variant an in-store barcode (starts with 2), ready for labels
            </label>
          )}
          {error && (
            <InlineAlert>
              {error.message}
              {collisions && collisions.length > 10 && ` (${collisions.length} in all)`}
            </InlineAlert>
          )}
          {preview && (
            <div className="rounded-md border p-3 text-sm" role="status">
              <p className="font-medium">
                {preview.planned.length} new variants{preview.skipped.length ? `; ${preview.skipped.length} already exist and are skipped` : ''}.
              </p>
              {preview.planned.length > 0 && (
                <table className="mt-2 w-full text-sm">
                  <thead className="text-left text-xs uppercase tracking-wide text-muted-foreground">
                    <tr>
                      <th scope="col" className="py-1 pr-3 font-medium">Variant</th>
                      <th scope="col" className="py-1 font-medium">SKU</th>
                    </tr>
                  </thead>
                  <tbody>
                    {preview.planned.map((p) => (
                      <tr key={p.sku}>
                        <td className="py-0.5 pr-3">{p.label}</td>
                        <td className="py-0.5 font-mono text-xs">{p.sku}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          )}
        </div>
        <DialogFooter className="gap-2">
          <Button type="button" variant="outline" className="max-sm:h-11" onClick={onClose}>
            Cancel
          </Button>
          <Button type="button" variant="outline" className="max-sm:h-11" disabled={busy} onClick={() => run(true)}>
            {busy && !preview && <Loader2 className="size-4 animate-spin" aria-hidden="true" />} Preview
          </Button>
          <Button type="button" className="max-sm:h-11" disabled={busy || !preview || !preview.planned.length} onClick={() => run(false)}>
            {busy && preview && <Loader2 className="size-4 animate-spin" aria-hidden="true" />} Create {preview ? preview.planned.length : ''} variants
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
