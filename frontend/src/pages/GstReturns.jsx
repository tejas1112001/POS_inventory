import { useEffect, useMemo, useState } from 'react';
import { FileSpreadsheet, Loader2 } from 'lucide-react';
import ReportTabs from '@/components/layout/ReportTabs';
import ListPageShell from '@/components/layout/ListPageShell';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { Button } from '@/components/ui/button';
import { usePermission } from '@/context/PermissionContext';
import { formatCurrency } from '@/lib/format';
import { api } from '@/services/api';

const selectCls = 'h-11 rounded-lg border border-input bg-card px-3 text-base sm:h-9 sm:text-sm';
const pad = (n) => String(n).padStart(2, '0');
const month = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}`;

// quarters are the last eight GST quarters (financial year: Q1 is April to
// June), newest first: { value: "2026-10|2026-12", label }.
function quarters(today = new Date()) {
  const out = [];
  let y = today.getFullYear();
  let q = Math.floor(((today.getMonth() + 9) % 12) / 3); // 0: Apr-Jun
  if (today.getMonth() < 3) y -= 1; // Jan-Mar belongs to the year that started last April
  for (let i = 0; i < 8; i++) {
    const startMonth = 3 + q * 3; // 3 = April (0-based)
    const s = new Date(y, startMonth, 1);
    const e = new Date(y, startMonth + 2, 1);
    const names = s.toLocaleString('en-IN', { month: 'short' }) + '–' + e.toLocaleString('en-IN', { month: 'short', year: 'numeric' });
    out.push({ value: `${month(s)}|${month(e)}`, label: `Q${q + 1} ${y}-${pad((y + 1) % 100)} (${names})` });
    q -= 1;
    if (q < 0) {
      q = 3;
      y -= 1;
    }
  }
  return out;
}

const SECTIONS = [
  ['b2b', 'B2B invoices'],
  ['b2cl', 'Large B2C to other states'],
  ['b2cs', 'Other B2C (by state and rate)'],
  ['cdnr', 'Credit notes, registered'],
  ['cdnur', 'Credit notes, large B2C'],
  ['hsn', 'HSN summary'],
  ['docs', 'Number series'],
];

// GstReturns (docs/GST_REPORTS_DESIGN.md): the GSTR-1 figures and the
// GSTR-3B summary of a GSTIN for a month or a quarter, and the workbook
// for the accountant. The app prepares the figures; it files nothing.
export default function GstReturns() {
  const { hasPermission } = usePermission();
  const [choices, setChoices] = useState(null);
  const [gstin, setGstin] = useState('');
  const [mode, setMode] = useState('month');
  const [monthValue, setMonthValue] = useState(month(new Date()));
  const qs = useMemo(() => quarters(), []);
  const [quarter, setQuarter] = useState(qs[0].value);
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);
  const [downloading, setDownloading] = useState(false);

  const [from, to] = mode === 'month' ? [monthValue, monthValue] : quarter.split('|');
  const query = `gstin=${encodeURIComponent(gstin)}&from=${from}&to=${to}`;

  useEffect(() => {
    api
      .get('/api/reports/gst/gstins', { quiet: true })
      .then((c) => {
        setChoices(c || []);
        if (c?.length) setGstin(c[0].gstin);
      })
      .catch((e) => setError(e));
  }, []);

  useEffect(() => {
    if (!gstin || !from) return undefined;
    let live = true;
    setBusy(true);
    setError(null);
    api
      .get(`/api/reports/gst?${query}`, { quiet: true })
      .then((d) => live && setData(d))
      .catch((e) => live && setError(e))
      .finally(() => live && setBusy(false));
    return () => {
      live = false;
    };
  }, [gstin, from, to, query]);

  const download = async () => {
    setDownloading(true);
    try {
      await api.download(`/api/reports/gst/export?${query}`, `GSTR1-${gstin}.xlsx`, { quiet: true });
    } catch (e) {
      setError(e);
    } finally {
      setDownloading(false);
    }
  };

  const g = data?.gstr3b;
  const tiles = g
    ? [
        ['Taxable value', g.outward.taxable],
        ['IGST', g.outward.igst],
        ['CGST', g.outward.cgst],
        ['SGST / UTGST', g.outward.sgst],
        ['Nil-rated (0 %)', g.nil],
      ]
    : [];

  return (
    <ListPageShell>
      <ReportTabs />
      <div className="space-y-4">
        <p className="text-sm text-muted-foreground">
          The figures for GSTR-1 and the GSTR-3B summary, from sales and credit notes made under a GST state. Check them with your accountant before
          filing; the app files nothing.
        </p>
        {choices && choices.length === 0 && (
          <InlineAlert kind="info">No GSTIN yet. Set the shop&apos;s GST state and GSTIN in Settings, or a store&apos;s own on the Stores page.</InlineAlert>
        )}
        <div className="flex flex-wrap items-end gap-3">
          <label className="flex flex-col gap-1 text-sm font-medium">
            GSTIN
            <select className={selectCls} value={gstin} onChange={(e) => setGstin(e.target.value)} disabled={!choices?.length}>
              {(choices || []).map((c) => (
                <option key={c.gstin} value={c.gstin}>
                  {c.gstin} · {c.state} · {c.label}
                </option>
              ))}
            </select>
          </label>
          <label className="flex flex-col gap-1 text-sm font-medium">
            Period
            <select className={selectCls} value={mode} onChange={(e) => setMode(e.target.value)}>
              <option value="month">A month</option>
              <option value="quarter">A quarter (QRMP)</option>
            </select>
          </label>
          {mode === 'month' ? (
            <label className="flex flex-col gap-1 text-sm font-medium">
              Month
              <input type="month" className={selectCls} value={monthValue} max={month(new Date())} onChange={(e) => e.target.value && setMonthValue(e.target.value)} />
            </label>
          ) : (
            <label className="flex flex-col gap-1 text-sm font-medium">
              Quarter
              <select className={selectCls} value={quarter} onChange={(e) => setQuarter(e.target.value)}>
                {qs.map((q) => (
                  <option key={q.value} value={q.value}>
                    {q.label}
                  </option>
                ))}
              </select>
            </label>
          )}
          {hasPermission('reports', 'export') && (
            <Button type="button" variant="outline" onClick={download} disabled={!gstin || downloading}>
              {downloading ? <Loader2 className="size-4 animate-spin" /> : <FileSpreadsheet className="size-4" />} Download GSTR-1 workbook
            </Button>
          )}
        </div>
        {error && <InlineAlert>{error.message || 'The GST report could not be loaded.'}</InlineAlert>}
        {busy && !data && <p className="text-sm text-muted-foreground">Loading…</p>}
        {data && (
          <>
            {data.warnings.map((w) => (
              <InlineAlert key={w} kind="warning">
                {w}
              </InlineAlert>
            ))}
            <section aria-label="GSTR-3B summary" className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
              {tiles.map(([label, v]) => (
                <div key={label} className="rounded-2xl border bg-card p-3 shadow-xs">
                  <p className="text-xs text-muted-foreground">{label}</p>
                  <p className="text-lg font-semibold tabular-nums">{formatCurrency(v)}</p>
                </div>
              ))}
            </section>
            <p className="text-xs text-muted-foreground">
              GSTR-3B 3.1(a) and (c), net of credit notes: {g.invoiceCount} invoice{g.invoiceCount === 1 ? '' : 's'}, {g.creditNoteCount} credit note
              {g.creditNoteCount === 1 ? '' : 's'}.
            </p>
            <section className="overflow-hidden rounded-2xl border bg-card shadow-xs" aria-label="GSTR-1 sections">
              <table className="w-full text-sm">
                <thead className="bg-muted text-left text-xs uppercase tracking-wide text-slate-600 dark:text-slate-300">
                  <tr>
                    <th scope="col" className="px-4 py-2 font-medium">GSTR-1 section</th>
                    <th scope="col" className="px-3 py-2 text-right font-medium">Rows</th>
                    <th scope="col" className="px-4 py-2 text-right font-medium">Taxable value</th>
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {SECTIONS.map(([k, label]) => {
                    const rows = data[k] || [];
                    const taxable = rows.reduce((s, r) => s + (Number(r.taxable) || 0), 0);
                    return (
                      <tr key={k}>
                        <td className="px-4 py-2">
                          {label} <span className="font-mono text-xs text-muted-foreground">({k})</span>
                        </td>
                        <td className="px-3 py-2 text-right tabular-nums">{rows.length}</td>
                        <td className="px-4 py-2 text-right tabular-nums">{k === 'docs' ? '—' : formatCurrency(taxable)}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </section>
          </>
        )}
      </div>
    </ListPageShell>
  );
}
