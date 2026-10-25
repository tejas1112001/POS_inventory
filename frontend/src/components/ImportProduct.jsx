import { useState } from 'react';
import { Download, FileCheck2, Loader2, Upload, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { api } from '@/services/api';

const MAX_BYTES = 5 * 1024 * 1024;

// ImportProduct uploads a product CSV (C-19, API_CONTRACT §5.1.1):
// "Check file" validates without saving (?dryRun=true), "Import" saves all
// rows or none. Row errors from the server are listed with their row.
export default function ImportProduct({ showForm, setShowForm, onImportComplete }) {
  const [file, setFile] = useState(null);
  const [busy, setBusy] = useState('');
  const [result, setResult] = useState(null); // {dryRun, created, variants}
  const [errors, setErrors] = useState([]);
  const [message, setMessage] = useState('');

  if (!showForm) return null;
  const close = () => setShowForm(false);

  const pick = (e) => {
    const f = e.target.files?.[0] || null;
    setResult(null);
    setErrors([]);
    setMessage('');
    if (f && f.size > MAX_BYTES) {
      setMessage('The file can be at most 5 MB.');
      setFile(null);
      return;
    }
    setFile(f);
  };

  const send = async (dryRun) => {
    if (!file) {
      setMessage('Choose a CSV file first.');
      return;
    }
    setBusy(dryRun ? 'check' : 'import');
    setResult(null);
    setErrors([]);
    setMessage('');
    const form = new FormData();
    form.append('csvFile', file);
    try {
      const res = await api.postFormData(`/api/products/import${dryRun ? '?dryRun=true' : ''}`, form, { quiet: true });
      setResult({ dryRun, created: res?.created ?? 0, variants: res?.variants ?? 0 });
      if (!dryRun) onImportComplete?.();
    } catch (err) {
      const rows = Array.isArray(err.body?.errors) ? err.body.errors : [];
      setErrors(rows);
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

  const template = async () => {
    try {
      await api.download('/api/products/import/template', 'products-template.csv', { quiet: true });
    } catch (err) {
      setMessage(err.message);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={close}>
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Import products"
        className="flex max-h-[90vh] w-full max-w-2xl flex-col overflow-hidden rounded-xl border bg-card shadow-xl"
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
              One row per product (or per variant: rows with the same name become variants). Required columns: name, sku, price.
              Categories, brands, units and stores must already exist. Existing SKUs or barcodes are not changed.
            </p>
            <p>All rows are imported or none: any error stops the whole file. At most 5,000 rows.</p>
          </div>

          <Button type="button" variant="outline" onClick={template}>
            <Download className="size-4" /> Download the sample file
          </Button>

          <label className="flex cursor-pointer flex-col items-center gap-2 rounded-lg border-2 border-dashed p-6 text-center hover:bg-accent/50">
            <Upload className="size-6 text-muted-foreground" />
            <span className="font-medium">{file ? file.name : 'Choose a CSV file'}</span>
            <span className="text-xs text-muted-foreground">UTF-8, comma-separated, up to 5 MB</span>
            <input type="file" accept=".csv,text/csv" className="sr-only" onChange={pick} aria-label="CSV file" />
          </label>

          {result && (
            <div role="status" className="rounded-md border border-emerald-300 bg-emerald-50 px-3 py-2 text-emerald-800">
              {result.dryRun
                ? `The file is fine: ${result.created} products (${result.variants} variants) would be created. Nothing was saved yet.`
                : `Imported ${result.created} products (${result.variants} variants).`}
            </div>
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
