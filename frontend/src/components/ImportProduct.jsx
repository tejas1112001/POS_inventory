import { useState } from 'react';
import { ChevronDown, ChevronRight, Download, FileCheck2, FileWarning, Loader2, Upload, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { useModules } from '@/hooks/useModules';
import { formatCurrency } from '@/lib/format';
import { api } from '@/services/api';

const MAX_BYTES = 5 * 1024 * 1024;
const TYPES = [
  ['retail', 'General (one row per product or option)'],
  ['clothing', 'Clothing (article number, size, colour…)'],
  ['footwear', 'Footwear (article number, size system, size, colour…)'],
  ['pharmacy', 'Pharmacy (one row per batch: batch number, expiry, MRP…)'],
];

// ImportProduct uploads a product file (C-19, API_CONTRACT §5.1.1; industry
// P1 item 6, A12-A14): .xlsx or .csv, using the template for the shop's
// type. "Check file" validates without saving (?dryRun=true) and shows what
// will be created; "Import" saves all rows or none. Row errors are listed
// and can be downloaded as a spreadsheet.
export default function ImportProduct({ showForm, setShowForm, onImportComplete }) {
  const { businessType, hasModule } = useModules();
  // The pharmacy template needs Batches & expiry (industry P3 item 8, A35).
  const types = TYPES.filter(([k]) => k !== 'pharmacy' || hasModule('batch_expiry'));
  const [type, setType] = useState(() => (types.some(([k]) => k === businessType) ? businessType : 'retail'));
  const [file, setFile] = useState(null);
  const [busy, setBusy] = useState('');
  const [result, setResult] = useState(null); // {dryRun, created, variants, preview, warnings}
  const [errors, setErrors] = useState([]);
  const [report, setReport] = useState('');
  const [message, setMessage] = useState('');
  const [open, setOpen] = useState({});

  if (!showForm) return null;
  const close = () => setShowForm(false);

  const reset = () => {
    setResult(null);
    setErrors([]);
    setReport('');
    setMessage('');
    setOpen({});
  };

  const pick = (e) => {
    const f = e.target.files?.[0] || null;
    reset();
    if (f && f.size > MAX_BYTES) {
      setMessage('The file can be at most 5 MB.');
      setFile(null);
      return;
    }
    setFile(f);
  };

  const send = async (dryRun) => {
    if (!file) {
      setMessage('Choose a file first.');
      return;
    }
    setBusy(dryRun ? 'check' : 'import');
    reset();
    const form = new FormData();
    form.append('file', file);
    try {
      const res = await api.postFormData(`/api/products/import${dryRun ? '?dryRun=true' : ''}`, form, { quiet: true });
      setResult({ dryRun, created: res?.created ?? 0, variants: res?.variants ?? 0, preview: res?.preview || [], warnings: res?.warnings || [] });
      if (!dryRun) onImportComplete?.();
    } catch (err) {
      const rows = Array.isArray(err.body?.errors) ? err.body.errors : [];
      setErrors(rows);
      setReport(err.body?.errorReport || '');
      setMessage(
        rows.length
          ? `Nothing was imported. Fix these rows and try again${rows.length >= 200 ? ' (only the first 200 are listed)' : ''}:`
          : err.code === 'PLAN_LIMIT_REACHED'
            ? 'Your plan does not allow this many products.'
            : err.message
      );
    } finally {
      setBusy('');
    }
  };

  const template = async (format) => {
    try {
      await api.download(`/api/products/import/template?type=${type}&format=${format}`, `products-${type}.${format}`, { quiet: true });
    } catch (err) {
      setMessage(err.message);
    }
  };

  const downloadReport = async () => {
    try {
      await api.download(`/api/products/import/errors/${encodeURIComponent(report)}`, 'import-errors.xlsx', { quiet: true });
    } catch (err) {
      setMessage(err.status === 404 ? 'The error report has expired (reports are kept for 1 hour). Check the file again.' : err.message);
    }
  };

  const preview = result?.preview || [];
  const stockText = (v) => (v.stock || []).filter((s) => s.qty > 0).map((s) => `${s.qty} at ${s.store}`).join(', ');

  return (
    <div className="fixed inset-0 z-50 flex justify-center overflow-y-auto overscroll-contain bg-[#0b2a32]/55 p-4 [&>*:last-child]:my-auto" onClick={close}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Import products"
        className="flex max-h-[90vh] w-full max-w-3xl flex-col overflow-hidden rounded-xl border bg-card shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b p-5">
          <h3 className="text-lg font-bold">Import products</h3>
          <button onClick={close} className="rounded-full p-1 text-muted-foreground hover:bg-accent" aria-label="Close">
            <X className="size-5" />
          </button>
        </div>

        <div className="flex-1 space-y-5 overflow-y-auto p-6 text-sm">
          <div className="space-y-1 text-muted-foreground">
            <p>
              One row per product or option. Rows with the same article number (or, without one, the same name) become options of one product. Required
              columns: name, sku, price. MRP (optional) is the printed maximum retail price, tax included. Categories, brands, units and stores must already exist. Existing SKUs or barcodes are not changed.
            </p>
            <p>All rows are imported or none: any error stops the whole file. At most 5,000 rows, 5 MB.</p>
          </div>

          <div className="flex flex-wrap items-end gap-2">
            <label className="grid gap-1.5 font-medium">
              Template
              <select className="h-11 rounded-lg border border-input bg-card px-3 sm:h-9" value={type} onChange={(e) => setType(e.target.value)}>
                {types.map(([k, label]) => (
                  <option key={k} value={k}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
            <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => template('xlsx')}>
              <Download className="size-4" /> Excel template
            </Button>
            <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => template('csv')}>
              <Download className="size-4" /> CSV template
            </Button>
          </div>

          <label className="flex cursor-pointer flex-col items-center gap-2 rounded-lg border-2 border-dashed p-6 text-center hover:bg-accent/50">
            <Upload className="size-6 text-muted-foreground" />
            <span className="font-medium">{file ? file.name : 'Choose an Excel (.xlsx) or CSV file'}</span>
            <span className="text-xs text-muted-foreground">The first sheet is read. CSV: UTF-8, comma-separated. Up to 5 MB.</span>
            <input
              type="file"
              accept=".xlsx,.csv,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
              className="sr-only"
              onChange={pick}
              aria-label="Product file"
            />
          </label>

          {result && (
            <div role="status" className="rounded-md border border-emerald-300 bg-emerald-50 px-3 py-2 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-200">
              {result.dryRun
                ? `The file is fine: ${result.created} products (${result.variants} options) would be created. Nothing was saved yet.`
                : `Imported ${result.created} products (${result.variants} options).`}
            </div>
          )}

          {result?.warnings?.length > 0 && (
            <div role="status" className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200">
              {result.warnings.length} row{result.warnings.length === 1 ? '' : 's'} to note:
              <ul className="mt-1 max-h-40 list-none space-y-0.5 overflow-y-auto text-xs">
                {result.warnings.map((r, i) => (
                  <li key={`${r.row}-${i}`}>
                    Row {r.row}: {r.message}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {result?.dryRun && preview.length > 0 && (
            <ul className="divide-y rounded-md border" aria-label="Products to create">
              {preview.map((p, i) => (
                <li key={`${p.row}-${i}`}>
                  <button
                    type="button"
                    className="flex min-h-11 w-full items-center gap-2 px-3 py-2 text-left hover:bg-accent/50"
                    aria-expanded={!!open[i]}
                    onClick={() => setOpen((o) => ({ ...o, [i]: !o[i] }))}
                  >
                    {open[i] ? <ChevronDown className="size-4 shrink-0" aria-hidden="true" /> : <ChevronRight className="size-4 shrink-0" aria-hidden="true" />}
                    <span className="min-w-0 flex-1">
                      <span className="font-medium">{p.name}</span>
                      {p.articleNo && <span className="text-muted-foreground"> · {p.articleNo}</span>}
                      {Object.values(p.attributes || {}).length > 0 && (
                        <span className="text-muted-foreground"> · {Object.values(p.attributes).join(', ')}</span>
                      )}
                    </span>
                    <span className="shrink-0 text-xs text-muted-foreground">
                      {p.variants.length} option{p.variants.length === 1 ? '' : 's'} · row {p.row}
                    </span>
                  </button>
                  {open[i] && (
                    <div className="overflow-x-auto px-3 pb-3">
                      <table className="w-full text-xs">
                        <thead className="text-left uppercase tracking-wide text-muted-foreground">
                          <tr>
                            <th scope="col" className="py-1 pr-3 font-medium">Row</th>
                            <th scope="col" className="py-1 pr-3 font-medium">Option</th>
                            <th scope="col" className="py-1 pr-3 font-medium">SKU</th>
                            <th scope="col" className="py-1 pr-3 font-medium">Barcode</th>
                            <th scope="col" className="py-1 pr-3 text-right font-medium">Price</th>
                            <th scope="col" className="py-1 pr-3 text-right font-medium">MRP</th>
                            <th scope="col" className="py-1 font-medium">Opening stock</th>
                          </tr>
                        </thead>
                        <tbody>
                          {p.variants.map((v) => (
                            <tr key={v.row}>
                              <td className="py-0.5 pr-3 tabular-nums">{v.row}</td>
                              <td className="py-0.5 pr-3">{v.label || '—'}</td>
                              <td className="py-0.5 pr-3 font-mono">{v.sku}</td>
                              <td className="py-0.5 pr-3 font-mono">{v.barcode || '—'}</td>
                              <td className="py-0.5 pr-3 text-right tabular-nums">{formatCurrency(v.price)}</td>
                              <td className="py-0.5 pr-3 text-right tabular-nums">{v.mrp != null ? formatCurrency(v.mrp) : '—'}</td>
                              <td className="py-0.5">{stockText(v) || '—'}</td>
                            </tr>
                          ))}
                          {p.variants.flatMap((v) =>
                            (v.batches || []).map((b) => (
                              <tr key={`b-${b.row}`} className={b.skipped ? 'text-muted-foreground line-through' : ''}>
                                <td className="py-0.5 pr-3 tabular-nums">{b.row}</td>
                                <td className="py-0.5 pr-3" colSpan={5}>
                                  Batch {b.batchNo} · Exp {b.expiry}
                                  {b.skipped ? ' · expired, not imported' : ''}
                                </td>
                                <td className="py-0.5">{b.qty > 0 ? `${b.qty} at ${b.store}` : '—'}</td>
                              </tr>
                            ))
                          )}
                        </tbody>
                      </table>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          )}

          {message && (
            <div role="alert" className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-destructive">
              {message}
              {errors.length > 0 && (
                <ul className="mt-2 max-h-56 list-none space-y-0.5 overflow-y-auto text-xs">
                  {errors.map((r, i) => (
                    <li key={`${r.row}-${r.field}-${i}`}>
                      Row {r.row}
                      {r.field ? `, ${r.field}` : ''}: {r.message}
                    </li>
                  ))}
                </ul>
              )}
              {report && (
                <Button type="button" variant="outline" size="sm" className="mt-2 max-sm:h-11" onClick={downloadReport}>
                  <FileWarning className="size-4" aria-hidden="true" /> Download the error report
                </Button>
              )}
            </div>
          )}
        </div>

        <div className="flex justify-end gap-2 border-t p-4">
          <Button type="button" variant="outline" onClick={close}>
            {result && !result.dryRun ? 'Close' : 'Cancel'}
          </Button>
          <Button type="button" variant="outline" onClick={() => send(true)} disabled={!!busy || !file}>
            {busy === 'check' ? <Loader2 className="size-4 animate-spin" /> : <FileCheck2 className="size-4" />} Check file
          </Button>
          <Button type="button" onClick={() => send(false)} disabled={!!busy || !file || (result && !result.dryRun)}>
            {busy === 'import' ? <Loader2 className="size-4 animate-spin" /> : <Upload className="size-4" />} Import
          </Button>
        </div>
      </div>
    </div>
  );
}
