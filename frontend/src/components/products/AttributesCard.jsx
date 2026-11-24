import { useEffect, useMemo, useState } from 'react';
import { Grid3x3, Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { api } from '@/services/api';
import { productUpdatePayload } from '@/lib/productPayload';
import { toast } from '../../../shared/toast/store';
import VariantMatrixDialog from './VariantMatrixDialog';

const inputCls =
  'h-11 w-full rounded-lg border border-input bg-card px-3 text-base outline-none focus-visible:border-primary focus-visible:ring-[3px] focus-visible:ring-ring/25 sm:h-9 sm:text-sm';

function ValueInput({ def, value, onChange, label }) {
  if (def.kind === 'choice') {
    return (
      <select className={inputCls} value={value || ''} onChange={(e) => onChange(e.target.value)} aria-label={label}>
        <option value="">—</option>
        {def.choices.map((c) => (
          <option key={c} value={c}>
            {c}
          </option>
        ))}
      </select>
    );
  }
  return <input className={inputCls} value={value || ''} onChange={(e) => onChange(e.target.value)} aria-label={label} maxLength={60} />;
}

// AttributesCard (industry P1 item 4): a product's article number,
// product-level attributes (Department, Season, Size system), its variant
// dimensions (Size × Colour) and each variant's values, and "Create
// variants". Shown when the product_attributes module is on.
export default function AttributesCard({ product, canEdit, onSaved }) {
  const [defs, setDefs] = useState(null);
  const [loadError, setLoadError] = useState('');
  const [article, setArticle] = useState(product.articleNo || '');
  const [attrs, setAttrs] = useState(product.attributes || {});
  const [dims, setDims] = useState(product.variantDims || []);
  const [values, setValues] = useState({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const [matrixOpen, setMatrixOpen] = useState(false);

  useEffect(() => {
    api
      .get('/api/attributes', { quiet: true })
      .then((d) => setDefs((d || []).filter((x) => x.status === 'Active' || (product.variantDims || []).includes(x.key) || x.key in (product.attributes || {}))))
      .catch((e) => setLoadError(e.message || 'Could not load the attributes.'));
  }, [product]);

  useEffect(() => {
    setArticle(product.articleNo || '');
    setAttrs(product.attributes || {});
    setDims(product.variantDims || []);
    const v = {};
    for (const x of product.ProductVariants || []) v[x.id] = { ...(x.attributes || {}) };
    setValues(v);
  }, [product]);

  const byKey = useMemo(() => Object.fromEntries((defs || []).map((d) => [d.key, d])), [defs]);
  if (loadError) return <InlineAlert>{loadError}</InlineAlert>;
  if (!defs) return null;

  const productDefs = defs.filter((d) => d.level === 'product');
  const variantDefs = defs.filter((d) => d.level === 'variant');
  const variants = product.ProductVariants || [];
  const toggleDim = (k) => setDims((cur) => (cur.includes(k) ? cur.filter((x) => x !== k) : [...cur, k]));
  const setValue = (vid, k, val) => setValues((cur) => ({ ...cur, [vid]: { ...cur[vid], [k]: val } }));

  const save = async () => {
    setBusy(true);
    setError(null);
    // With dimensions, each variant keeps only its dimension values.
    const va = {};
    for (const v of variants) {
      va[v.id] = dims.length ? Object.fromEntries(dims.map((k) => [k, values[v.id]?.[k] ?? ''])) : v.attributes || {};
    }
    try {
      await api.put(`/api/products/${product.id}`, productUpdatePayload(product, { attributes: attrs, variantDims: dims, articleNo: article }, va), { quiet: true });
      toast.success('Attributes saved.');
      onSaved();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  const fieldErrors = Object.entries(error?.fields || {});
  return (
    <section className="rounded-2xl border bg-card p-4 shadow-xs" aria-labelledby="attrs-h">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h2 id="attrs-h" className="text-base font-semibold">
          Sizes, colours and attributes
        </h2>
        {canEdit && product.productType === 'variable' && (product.variantDims || []).length > 0 && (
          <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => setMatrixOpen(true)}>
            <Grid3x3 className="size-4" aria-hidden="true" /> Create variants
          </Button>
        )}
      </div>
      {!defs.length && <p className="text-sm text-muted-foreground">No attributes are defined yet. Add them under Catalogue setup → Attributes.</p>}
      <fieldset disabled={!canEdit || busy} className="grid gap-4">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <label className="grid gap-1.5 text-sm font-medium">
            Article (style) number
            <input className={inputCls} value={article} onChange={(e) => setArticle(e.target.value)} maxLength={64} />
          </label>
          {productDefs.map((d) => (
            <label key={d.key} className="grid gap-1.5 text-sm font-medium">
              {d.label}
              <ValueInput def={d} value={attrs[d.key]} label={d.label} onChange={(val) => setAttrs((cur) => ({ ...cur, [d.key]: val }))} />
            </label>
          ))}
        </div>
        {variantDefs.length > 0 && (
          <div>
            <p className="mb-2 text-sm font-medium">Variants differ by</p>
            <div className="flex flex-wrap gap-3">
              {variantDefs.map((d) => (
                <label key={d.key} className="flex items-center gap-2 text-sm">
                  <input type="checkbox" className="size-4" checked={dims.includes(d.key)} onChange={() => toggleDim(d.key)} />
                  {d.label}
                </label>
              ))}
            </div>
            {product.productType !== 'variable' && dims.length > 0 && (
              <p className="mt-2 text-xs text-muted-foreground">A single product has one variant; to sell several sizes, create it as a product with variants.</p>
            )}
          </div>
        )}
        {dims.length > 0 && variants.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-xs uppercase tracking-wide text-muted-foreground">
                <tr>
                  <th scope="col" className="py-1 pr-3 font-medium">SKU</th>
                  {dims.map((k) => (
                    <th key={k} scope="col" className="py-1 pr-3 font-medium">
                      {byKey[k]?.label || k}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {variants.map((v) => (
                  <tr key={v.id}>
                    <td className="py-1 pr-3 text-muted-foreground">{v.sku}</td>
                    {dims.map((k) => (
                      <td key={k} className="py-1 pr-3">
                        {byKey[k] && <ValueInput def={byKey[k]} value={values[v.id]?.[k]} label={`${byKey[k].label} of ${v.sku}`} onChange={(val) => setValue(v.id, k, val)} />}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {error && (
          <InlineAlert>
            {error.message}
            {fieldErrors.length > 1 && (
              <ul className="mt-1 list-disc pl-5">
                {fieldErrors.slice(0, 8).map(([k, m]) => (
                  <li key={k}>{m}</li>
                ))}
              </ul>
            )}
          </InlineAlert>
        )}
        {canEdit && (
          <div className="flex justify-end">
            <Button type="button" onClick={save} className="max-sm:h-11">
              {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />} Save attributes
            </Button>
          </div>
        )}
      </fieldset>
      {matrixOpen && <VariantMatrixDialog product={product} defs={byKey} onClose={() => setMatrixOpen(false)} onCreated={onSaved} />}
    </section>
  );
}
