import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, Ban, CheckCheck, ChevronDown, ChevronRight, Loader2, Printer, ScanLine, Upload, X } from 'lucide-react';
import { SectionCard } from '@/components/shared/section-card';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import PrintLabelsDialog from '@/components/products/PrintLabelsDialog';
import CountStatus from '@/components/stock/CountStatus';
import CountBatchDialog from '@/components/stock/CountBatchDialog';
import { usePermission } from '@/context/PermissionContext';
import { useModules } from '@/hooks/useModules';
import { useScanner } from '@/hooks/useScanner';
import { beep } from '@/lib/scan';
import { formatDateTime } from '@/lib/format';
import { cn } from '@/lib/utils';
import { api } from '@/services/api';
import { toast } from '../../shared/toast/store';

const inputCls =
  'h-11 w-full rounded-md border border-input bg-background px-3 text-base outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 sm:h-9 sm:text-sm';

const signed = (n) => (n > 0 ? `+${n}` : String(n));
const itemName = (l) => `${l.productName}${l.label ? ` · ${l.label}` : ''}`;

// StockCountDetail is one stock count (industry P1 item 7, A15). In a
// draft, a scan adds 1 to the item's count, a typed number replaces it, and
// a sheet (code, counted_qty) sets many at once. The review shows each
// difference against the stock now, changes since the item was counted,
// and what was not counted (left unchanged). Completing posts the
// differences to stock.
export default function StockCountDetail() {
  const { id } = useParams();
  const { hasPermission } = usePermission();
  const { hasModule } = useModules();
  const [count, setCount] = useState(null);
  const [lines, setLines] = useState([]);
  const [version, setVersion] = useState(0);
  const [error, setError] = useState(null);
  const [actionError, setActionError] = useState(null);
  const [rowErrors, setRowErrors] = useState([]);
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState('');
  const [confirm, setConfirm] = useState('');
  const [showUncounted, setShowUncounted] = useState(false);
  const [labels, setLabels] = useState(false);
  const [addBatch, setAddBatch] = useState(null); // a line: count one of its batches (industry P3 item 6)
  const fileRef = useRef(null);

  const draft = count?.status === 'Draft';
  const canEdit = draft && hasPermission('stock_counts', 'create');
  const canComplete = draft && hasPermission('stock_counts', 'complete');
  const canLabels = hasModule('barcode_labels') && hasPermission('labels', 'print');

  const load = useCallback(() => {
    return api
      .get(`/api/stock-counts/${id}`, { quiet: true })
      .then((c) => {
        setCount(c);
        setLines(c.lines || []);
        setVersion(c.version);
        setError(null);
      })
      .catch((err) => setError(err));
  }, [id]);

  useEffect(() => {
    load();
  }, [load]);

  // change sends line changes; the answer holds only the changed lines.
  const change = async (body, { quietError } = {}) => {
    setActionError(null);
    try {
      const r = await api.put(`/api/stock-counts/${id}`, body, { quiet: true });
      setVersion(r.version);
      // The answer holds every line of the changed items (a product counted
      // by batch has several), so they replace that item's lines.
      const touched = new Set((r.lines || []).map((l) => l.variantId));
      const gone = new Set(r.removed || []);
      const goneLines = new Set(r.removedLines || []);
      setLines((cur) => [
        ...cur.filter((l) => !touched.has(l.variantId) && !gone.has(l.variantId) && !goneLines.has(l.id)),
        ...(r.lines || []),
      ]);
      setCount((c) => (c ? { ...c, lineCount: r.lineCount } : c));
      // Batch changes move what completion sets to 0: refresh the summary.
      if ((body.lines || []).some((l) => l.batchId || l.batchNo) || body.removeLines?.length) {
        api.get(`/api/stock-counts/${id}`, { quiet: true }).then((c) => setCount(c)).catch(() => {});
      }
      return r;
    } catch (err) {
      if (!quietError) setActionError(err);
      throw err;
    }
  };

  const scan = async (value) => {
    const c = value.trim();
    if (!c) return;
    try {
      const r = await change({ lines: [{ code: c, add: 1 }] }, { quietError: true });
      const l = r.lines?.[0];
      if (l) toast.success(`${itemName(l)}: ${l.countedQty}`);
    } catch (err) {
      beep();
      toast.error(err.message);
    }
  };

  useScanner((c) => scan(c), { enabled: canEdit });

  const setQty = async (line, value) => {
    const n = Math.floor(Number(value));
    if (value === '' || !Number.isFinite(n) || n < 0 || n === line.countedQty) return;
    await change({ lines: [{ variantId: line.variantId, ...(line.batchId ? { batchId: line.batchId } : {}), qty: n }] }).catch(() => {});
  };

  const countZero = async (u) => {
    await change({ lines: [{ variantId: u.variantId, qty: 0 }] })
      .then(() => setCount((c) => ({ ...c, uncounted: c.uncounted.filter((x) => x.variantId !== u.variantId), uncountedTotal: c.uncountedTotal - 1 })))
      .catch(() => {});
  };

  const importFile = async (file) => {
    if (!file) return;
    setBusy('import');
    setActionError(null);
    setRowErrors([]);
    const form = new FormData();
    form.append('file', file);
    try {
      const r = await api.postFormData(`/api/stock-counts/${id}/import`, form, { quiet: true });
      toast.success(`${r.imported} item${r.imported === 1 ? '' : 's'} counted from the file.`);
      await load();
    } catch (err) {
      setActionError(err);
      setRowErrors(Array.isArray(err.body?.errors) ? err.body.errors : []);
    } finally {
      setBusy('');
      if (fileRef.current) fileRef.current.value = '';
    }
  };

  // Expired batches found on the shelf: one write-off from the result (A30).
  const expiredFound = !draft && count?.status === 'Completed' ? lines.filter((l) => l.batchId && l.expired && l.countedQty > 0) : [];
  const canWriteOff = hasModule('batch_expiry') && hasPermission('batches', 'write_off');
  const writeOffExpired = async () => {
    setBusy('writeoff');
    setActionError(null);
    try {
      const w = await api.post(
        '/api/write-offs',
        { storeId: count.storeId, reason: 'expired', note: `Found in ${count.reference}`, lines: expiredFound.map((l) => ({ batchId: l.batchId, quantity: l.countedQty, from: 'stock' })) },
        { quiet: true },
      );
      toast.success(`Written off as ${w.reference}.`);
    } catch (err) {
      setActionError(err);
    } finally {
      setBusy('');
    }
  };

  const finish = async (what) => {
    setBusy(what);
    setActionError(null);
    try {
      const c = await api.post(`/api/stock-counts/${id}/${what}`, { version }, { quiet: true });
      setCount(c);
      setLines(c.lines || []);
      setVersion(c.version);
      setConfirm('');
      toast.success(what === 'complete' ? 'Count completed: stock updated.' : 'Count cancelled.');
    } catch (err) {
      setConfirm('');
      setActionError(err);
      if (err.status === 409) load();
    } finally {
      setBusy('');
    }
  };

  if (error) {
    return <InlineAlert>{error.status === 404 ? 'This stock count does not exist.' : error.message}</InlineAlert>;
  }
  if (!count) {
    return <Loader2 className="mx-auto mt-10 size-6 animate-spin text-muted-foreground" aria-label="Loading" />;
  }

  const totals = lines.reduce(
    (t, l) => {
      if (l.difference > 0) t.up += l.difference;
      if (l.difference < 0) t.down -= l.difference;
      if (l.difference !== 0) t.changed += 1;
      return t;
    },
    { up: 0, down: 0, changed: 0 }
  );
  const shifted = draft ? lines.filter((l) => l.movedSince !== 0).length : 0;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <Link to="/stock-counts" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:underline">
            <ArrowLeft className="size-4" aria-hidden="true" /> Stock counts
          </Link>
          <h2 className="flex flex-wrap items-center gap-2 text-xl font-semibold">
            {count.reference} <CountStatus status={count.status} />
          </h2>
          <p className="text-sm text-muted-foreground">
            {count.Store?.name} · {count.scopeText} · started {formatDateTime(count.createdAt)}
            {count.createdBy ? ` by ${count.createdBy.name}` : ''}
            {count.completedAt ? ` · completed ${formatDateTime(count.completedAt)}${count.completedBy ? ` by ${count.completedBy.name}` : ''}` : ''}
          </p>
          {count.note && <p className="text-sm">{count.note}</p>}
        </div>
        <div className="flex flex-wrap gap-2">
          {canLabels && lines.length > 0 && (
            <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => setLabels(true)}>
              <Printer className="size-4" aria-hidden="true" /> Labels
            </Button>
          )}
          {canEdit && (
            <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => setConfirm('cancel')} disabled={!!busy}>
              <Ban className="size-4" aria-hidden="true" /> Cancel count
            </Button>
          )}
          {canComplete && (
            <Button type="button" className="max-sm:h-11" onClick={() => setConfirm('complete')} disabled={!!busy || lines.length === 0}>
              <CheckCheck className="size-4" aria-hidden="true" /> Review and complete
            </Button>
          )}
        </div>
      </div>

      {canEdit && (
        <SectionCard>
          <div className="flex flex-wrap items-end gap-2 pt-4">
            <form
              className="flex min-w-60 flex-1 items-end gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                scan(code);
                setCode('');
              }}
            >
              <label className="grid flex-1 gap-1.5 text-sm font-medium">
                Scan or type a barcode or SKU
                <input className={inputCls} value={code} onChange={(e) => setCode(e.target.value)} autoComplete="off" placeholder="Each scan adds 1" />
              </label>
              <Button type="submit" variant="outline" className="max-sm:h-11">
                <ScanLine className="size-4" aria-hidden="true" /> Add 1
              </Button>
            </form>
            <input
              ref={fileRef}
              type="file"
              accept=".xlsx,.csv,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
              className="sr-only"
              aria-label="Count sheet"
              onChange={(e) => importFile(e.target.files?.[0])}
            />
            <Button type="button" variant="outline" className="max-sm:h-11" disabled={!!busy} onClick={() => fileRef.current?.click()}>
              {busy === 'import' ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <Upload className="size-4" aria-hidden="true" />} Upload a
              count sheet
            </Button>
          </div>
          <p className="pb-3 pt-2 text-xs text-muted-foreground">
            With a scanner you can scan anywhere on this page. A sheet needs two columns: <strong>sku</strong> (or barcode) and{' '}
            <strong>counted_qty</strong>; it replaces the counts of the items in it. Nothing changes stock until the count is completed.
          </p>
        </SectionCard>
      )}

      {actionError && (
        <InlineAlert>
          {actionError.message}
          {rowErrors.length > 0 && (
            <ul className="mt-2 max-h-48 list-none space-y-0.5 overflow-y-auto text-xs">
              {rowErrors.map((r, i) => (
                <li key={`${r.row}-${i}`}>
                  Row {r.row}
                  {r.field ? `, ${r.field}` : ''}: {r.message}
                </li>
              ))}
            </ul>
          )}
        </InlineAlert>
      )}

      {shifted > 0 && (
        <InlineAlert kind="warning">
          Stock of {shifted} counted item{shifted === 1 ? ' has' : 's have'} changed since {shifted === 1 ? 'it was' : 'they were'} counted (a sale, a
          purchase…). Completing sets the counted quantity anyway; count {shifted === 1 ? 'it' : 'them'} again if the shelf changed too.
        </InlineAlert>
      )}

      <SectionCard
        title={`Counted items (${lines.length})`}
        action={
          lines.length > 0 && (
            <span className="text-xs text-muted-foreground">
              {totals.changed} differ{totals.changed === 1 ? 's' : ''} · +{totals.up} / −{totals.down} units
            </span>
          )
        }
      >
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase tracking-wide text-muted-foreground">
              <tr>
                <th scope="col" className="px-3 py-2 font-medium">Item</th>
                <th scope="col" className="w-28 px-3 py-2 font-medium">Counted</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">{draft ? 'In stock now' : 'Stock before'}</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Difference</th>
                {canEdit && (
                  <th scope="col" className="w-10 px-3 py-2">
                    <span className="sr-only">Remove</span>
                  </th>
                )}
              </tr>
            </thead>
            <tbody className="divide-y">
              {lines.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-3 py-6 text-center text-muted-foreground">
                    {canEdit ? 'Scan an item to start.' : 'Nothing was counted.'}
                  </td>
                </tr>
              )}
              {lines.map((l) => (
                <tr key={l.id}>
                  <td className="px-3 py-2">
                    <div className="font-medium">{l.productName}</div>
                    <div className="text-xs text-muted-foreground">
                      {l.label ? `${l.label} · ` : ''}
                      {l.sku}
                      {l.barcode ? ` · ${l.barcode}` : ''}
                    </div>
                    {(l.batchId || l.byBatch) && (
                      <div className={cn('text-xs', l.expired ? 'font-medium text-destructive' : 'text-muted-foreground')}>
                        {l.batchId ? `Batch ${l.batchNo}${l.expiry ? ` · Exp ${l.expiry}` : ''}${l.expired ? ' · expired' : ''}` : 'No batch'}
                      </div>
                    )}
                    {canEdit && l.tracksBatches && (
                      <button type="button" className="text-xs font-medium text-primary underline-offset-2 hover:underline" onClick={() => setAddBatch(l)}>
                        Count a batch
                      </button>
                    )}
                    {draft && l.movedSince !== 0 && (
                      <div className="text-xs text-amber-700 dark:text-amber-400">Stock changed by {signed(l.movedSince)} since counted</div>
                    )}
                  </td>
                  <td className="px-3 py-2">
                    {canEdit ? (
                      <input
                        key={`${l.id}-${l.countedQty}`}
                        type="number"
                        min="0"
                        className={inputCls}
                        defaultValue={l.countedQty}
                        aria-label={`Counted ${itemName(l)}`}
                        onBlur={(e) => setQty(l, e.target.value)}
                        onKeyDown={(e) => e.key === 'Enter' && e.currentTarget.blur()}
                      />
                    ) : (
                      <span className="tabular-nums">{l.countedQty}</span>
                    )}
                  </td>
                  <td className="px-3 py-2 text-right tabular-nums">{draft ? l.currentQty : (l.expected ?? '—')}</td>
                  <td
                    className={cn(
                      'px-3 py-2 text-right font-medium tabular-nums',
                      l.difference > 0 && 'text-emerald-700 dark:text-emerald-400',
                      l.difference < 0 && 'text-destructive'
                    )}
                  >
                    {count.status === 'Cancelled' ? '—' : signed(l.difference)}
                  </td>
                  {canEdit && (
                    <td className="px-3 py-2">
                      <button
                        type="button"
                        className="rounded p-1.5 text-muted-foreground hover:bg-accent"
                        aria-label={`Remove ${itemName(l)}`}
                        onClick={() => change({ removeLines: [l.id] }).catch(() => {})}
                      >
                        <X className="size-4" />
                      </button>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </SectionCard>

      {draft && count.uncountedBatches?.length > 0 && (
        <InlineAlert kind="warning">
          These batches have no line and will be set to 0 when you complete (their products are counted by batch):{' '}
          {count.uncountedBatches.map((b) => `${b.productName} ${b.unknown ? 'No batch' : b.batchNo} (${b.currentQty})`).join(', ')}.
        </InlineAlert>
      )}
      {expiredFound.length > 0 && canWriteOff && (
        <InlineAlert kind="warning">
          {expiredFound.length} expired batch{expiredFound.length === 1 ? ' was' : 'es were'} found on the shelf.{' '}
          <Button type="button" size="sm" variant="destructive" className="ml-2" disabled={!!busy} onClick={writeOffExpired}>
            {busy === 'writeoff' && <Loader2 className="size-4 animate-spin" aria-hidden="true" />} Write off expired
          </Button>
        </InlineAlert>
      )}
      {addBatch && (
        <CountBatchDialog line={addBatch} storeId={count.storeId} onAdd={(l) => change({ lines: [l] }, { quietError: true })} onClose={() => setAddBatch(null)} />
      )}

      {draft && (count.uncountedTotal ?? 0) > 0 && (
        <SectionCard>
          <button
            type="button"
            className="flex min-h-11 w-full items-center gap-2 py-3 text-left text-sm font-medium"
            aria-expanded={showUncounted}
            onClick={() => setShowUncounted((s) => !s)}
          >
            {showUncounted ? <ChevronDown className="size-4" aria-hidden="true" /> : <ChevronRight className="size-4" aria-hidden="true" />}
            Not counted yet: {count.uncountedTotal} item{count.uncountedTotal === 1 ? '' : 's'} (left unchanged when you complete)
          </button>
          {showUncounted && (
            <div className="overflow-x-auto pb-3">
              <table className="w-full text-sm">
                <tbody className="divide-y">
                  {count.uncounted.map((u) => (
                    <tr key={u.variantId}>
                      <td className="px-3 py-1.5">
                        <div>{u.productName}</div>
                        <div className="text-xs text-muted-foreground">
                          {u.label ? `${u.label} · ` : ''}
                          {u.sku}
                        </div>
                      </td>
                      <td className="px-3 py-1.5 text-right tabular-nums">{u.currentQty} in stock</td>
                      {canEdit && (
                        <td className="w-32 px-3 py-1.5 text-right">
                          <Button type="button" size="sm" variant="outline" onClick={() => countZero(u)}>
                            None on shelf
                          </Button>
                        </td>
                      )}
                    </tr>
                  ))}
                </tbody>
              </table>
              {count.uncountedTotal > count.uncounted.length && (
                <p className="px-3 pt-2 text-xs text-muted-foreground">Showing the {count.uncounted.length} with the most stock.</p>
              )}
            </div>
          )}
        </SectionCard>
      )}

      {confirm && (
        <Dialog open onOpenChange={(o) => !o && setConfirm('')}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>{confirm === 'complete' ? `Complete ${count.reference}?` : `Cancel ${count.reference}?`}</DialogTitle>
              <DialogDescription>
                {confirm === 'complete'
                  ? `The stock of ${lines.length} counted item${lines.length === 1 ? '' : 's'} becomes the counted quantity: ${totals.changed} change${totals.changed === 1 ? 's' : ''}, +${totals.up} and −${totals.down} units. Items not counted are left as they are. This cannot be undone.`
                  : 'The count is kept for reference but changes no stock, and it cannot be edited again.'}
              </DialogDescription>
            </DialogHeader>
            {confirm === 'complete' && shifted > 0 && (
              <InlineAlert kind="warning">
                {shifted} item{shifted === 1 ? ' was' : 's were'} sold or received since counting. Their counted quantity is used as it is.
              </InlineAlert>
            )}
            <DialogFooter className="gap-2">
              <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => setConfirm('')}>
                Back
              </Button>
              <Button
                type="button"
                variant={confirm === 'cancel' ? 'destructive' : 'default'}
                className="max-sm:h-11"
                disabled={!!busy}
                onClick={() => finish(confirm)}
              >
                {busy === confirm && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
                {confirm === 'complete' ? 'Complete and update stock' : 'Cancel count'}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}

      {labels && (
        <PrintLabelsDialog
          title={count.reference}
          rows={lines.map((l) => ({
            key: l.id,
            id: l.variantId,
            name: itemName(l),
            sku: l.sku,
            itemBarcode: l.barcode || null,
            copies: l.countedQty,
            ...(l.batchId ? { batchId: l.batchId, batchLabel: `Batch ${l.batchNo}` } : {}),
          }))}
          onClose={() => setLabels(false)}
        />
      )}
    </div>
  );
}
