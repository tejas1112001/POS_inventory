import { useEffect, useMemo, useState } from 'react';
import { Plus, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { api } from '@/services/api';
import { toast } from '../../../shared/toast/store';

const CODE = /^[A-Za-z0-9._/+-]{1,64}$/;

function rowsOf(product) {
  const out = [];
  for (const v of product?.ProductVariants || []) {
    for (const b of v.barcodes || []) out.push({ variantId: String(v.id), code: b.code, unitId: b.unitId ? String(b.unitId) : '' });
  }
  return out;
}

// BarcodesCard edits a product's other barcodes (several barcodes, tenant
// 00043): an old EAN, a supplier's code, a case code. A code may name one
// of the product's units, so scanning it counts one case. The main
// barcode (labels print it) is set on the product form.
export default function BarcodesCard({ product, canEdit, onSaved }) {
  const saved = useMemo(() => rowsOf(product), [product]);
  const [rows, setRows] = useState(saved);
  const [busy, setBusy] = useState(false);
  useEffect(() => setRows(saved), [saved]);
  const variants = product?.ProductVariants || [];
  const units = product?.units || [];
  if (variants.length === 0 || (!canEdit && saved.length === 0)) return null;

  const set = (i, patch) => setRows((r) => r.map((x, j) => (j === i ? { ...x, ...patch } : x)));
  const label = (v) => v.label || v.sku;
  const problem = (() => {
    const seen = new Set();
    for (const [i, r] of rows.entries()) {
      const c = r.code.trim();
      if (!CODE.test(c)) return `Barcode ${i + 1}: letters, digits and . _ / + - only (at most 64).`;
      if (seen.has(c)) return `Barcode ${i + 1} is listed twice.`;
      if (variants.some((v) => v.itemBarcode === c)) return `Barcode ${i + 1} is already the main barcode.`;
      seen.add(c);
    }
    return '';
  })();
  const dirty = JSON.stringify(rows) !== JSON.stringify(saved);

  const save = async () => {
    setBusy(true);
    try {
      await api.put(
        `/api/products/${product.id}/barcodes`,
        { barcodes: rows.map((r) => ({ variantId: Number(r.variantId), code: r.code.trim(), ...(r.unitId ? { unitId: Number(r.unitId) } : {}) })) },
        { quiet: true }
      );
      toast.success('Barcodes saved.');
      onSaved?.();
    } catch (err) {
      const first = err?.fields && Object.values(err.fields)[0];
      toast.error(first || err.message || 'The barcodes could not be saved.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="overflow-hidden rounded-lg border bg-card shadow-xs" aria-labelledby="bc-h">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b px-4 py-3">
        <div>
          <h2 id="bc-h" className="text-base font-semibold">
            Other barcodes
          </h2>
          <p className="text-xs text-muted-foreground">Old or supplier codes, and case codes: a code with a unit counts one of that unit when scanned.</p>
        </div>
        {canEdit && (
          <div className="flex gap-2">
            <Button size="sm" variant="outline" onClick={() => setRows((r) => [...r, { variantId: String(variants[0].id), code: '', unitId: '' }])}>
              <Plus className="size-4" aria-hidden="true" /> Add
            </Button>
            <Button size="sm" onClick={save} disabled={busy || !dirty || Boolean(problem)}>
              {busy ? 'Saving…' : 'Save barcodes'}
            </Button>
          </div>
        )}
      </div>
      {rows.length === 0 ? (
        <p className="px-4 py-3 text-sm text-muted-foreground">No other barcodes.</p>
      ) : (
        <ul className="divide-y">
          {rows.map((r, i) => (
            <li key={i} className="flex flex-wrap items-center gap-2 px-4 py-2">
              {variants.length > 1 && (
                <select
                  className="h-8 rounded-md border bg-background px-2 text-sm"
                  value={r.variantId}
                  disabled={!canEdit}
                  onChange={(e) => set(i, { variantId: e.target.value })}
                  aria-label={`Option of barcode ${i + 1}`}
                >
                  {variants.map((v) => (
                    <option key={v.id} value={v.id}>
                      {label(v)}
                    </option>
                  ))}
                </select>
              )}
              <Input
                className="h-8 w-56 font-mono text-sm"
                value={r.code}
                disabled={!canEdit}
                maxLength={64}
                onChange={(e) => set(i, { code: e.target.value })}
                aria-label={`Barcode ${i + 1}`}
              />
              <select
                className="h-8 rounded-md border bg-background px-2 text-sm"
                value={r.unitId}
                disabled={!canEdit || units.length === 0}
                onChange={(e) => set(i, { unitId: e.target.value })}
                aria-label={`Unit of barcode ${i + 1}`}
              >
                <option value="">Single</option>
                {units.map((u) => (
                  <option key={u.unitId} value={u.unitId}>
                    {u.name} ({Number(u.factor)})
                  </option>
                ))}
              </select>
              {canEdit && (
                <Button variant="ghost" size="icon" className="size-8 text-muted-foreground hover:text-danger" aria-label={`Remove barcode ${i + 1}`} onClick={() => setRows((x) => x.filter((_, j) => j !== i))}>
                  <Trash2 className="size-4" aria-hidden="true" />
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {canEdit && problem && rows.length > 0 && (
        <p role="alert" className="border-t px-4 py-2 text-xs text-danger-fg">
          {problem}
        </p>
      )}
    </section>
  );
}
