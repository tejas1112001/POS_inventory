import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Loader2 } from 'lucide-react';
import ReportTabs from '@/components/layout/ReportTabs';
import ListPageShell from '@/components/layout/ListPageShell';
import ReportSelect from '@/components/reports/ReportSelect';
import { ReportActions } from '@/components/reports/ReportToolbar';
import BatchDialog from '@/components/batches/BatchDialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { usePermission } from '@/context/PermissionContext';
import { queryString } from '@/hooks/useServerReport';
import { formatCurrency } from '@/lib/format';
import { cn } from '@/lib/utils';
import { api } from '@/services/api';

const SECTIONS = [
  { key: 'expired', label: 'Expired on hand', tone: 'text-destructive' },
  {
    key: 'expiring',
    label: 'Expiring soon',
    tone: 'text-amber-700 dark:text-amber-400',
  },
  { key: 'quarantined', label: 'Quarantined', tone: '' },
  { key: 'recalled', label: 'Recalled with stock', tone: 'text-destructive' },
];
const WINDOWS = ['30', '60', '90', '180'];

// ExpiryReport (industry P3 item 7, A33): what is expired, about to
// expire, quarantined or recalled, per batch and store, with cost and MRP
// value. The window defaults to the shop's near-expiry days.
export default function ExpiryReport() {
  const { hasPermission } = usePermission();
  const [params, setParams] = useSearchParams();
  const section = params.get('section') || '';
  const [storeId, setStoreId] = useState('');
  const [windowDays, setWindowDays] = useState('');
  const [stores, setStores] = useState([]);
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);
  const [open, setOpen] = useState(null);
  const [reload, setReload] = useState(0);

  useEffect(() => {
    api
      .get('/api/stores', { quiet: true })
      .then((s) => setStores(s || []))
      .catch(() => {});
  }, []);
  const qs = queryString({ storeId, windowDays, section });
  useEffect(() => {
    let active = true;
    setError(null);
    api
      .get(`/api/reports/expiry${qs}`, { quiet: true })
      .then((d) => active && setData(d))
      .catch((err) => active && setError(err));
    return () => {
      active = false;
    };
  }, [qs, reload]);

  const win = windowDays || (data ? String(data.windowDays) : '');
  const windows = useMemo(() => [...new Set([...WINDOWS, win].filter(Boolean))].sort((a, b) => a - b), [win]);
  const setSection = (s) => {
    const p = new URLSearchParams(params);
    if (s && s !== section) p.set('section', s);
    else p.delete('section');
    setParams(p, { replace: true });
  };

  return (
    <ListPageShell scroll>
      <ReportTabs />
      <div className="space-y-4">
        <div className="flex flex-wrap items-center gap-2">
          <ReportSelect
            id="expiry-store"
            label="Store"
            value={storeId || 'all'}
            onChange={(v) => setStoreId(v === 'all' ? '' : v)}
            options={[{ value: 'all', label: 'All stores' }, ...stores.map((s) => ({ value: String(s.id), label: s.name }))]}
          />
          <ReportSelect
            id="expiry-window"
            label="Expiring within"
            value={win}
            onChange={setWindowDays}
            options={windows.map((w) => ({ value: w, label: `${w} days` }))}
            empty="Loading…"
          />
          <div className="flex flex-wrap items-center gap-2 max-sm:w-full max-sm:[&>*]:flex-1 sm:ml-auto sm:[&>*]:h-9 sm:[&>*]:text-[13px]">
            <ReportActions server={{ path: '/api/reports/expiry/export', qs, name: 'expiry-report', csv: true }} />
          </div>
        </div>

        {error && <InlineAlert>{error.message}</InlineAlert>}
        {!data && !error && <Loader2 className="mx-auto size-6 animate-spin text-muted-foreground" aria-label="Loading" />}
        {data && (
          <>
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              {SECTIONS.map((s) => {
                const v = data.summary?.[s.key] || {};
                return (
                  <button
                    key={s.key}
                    type="button"
                    aria-pressed={section === s.key}
                    onClick={() => setSection(s.key)}
                    className={cn(
                      'rounded-2xl border bg-card p-3 text-left shadow-xs hover:border-primary/40',
                      section === s.key && 'border-primary ring-1 ring-primary'
                    )}
                  >
                    <span className="block text-xs text-muted-foreground">{s.key === 'expiring' ? `Expiring within ${data.windowDays} days` : s.label}</span>
                    <span className={cn('block text-xl font-semibold tabular-nums', v.units > 0 && s.tone)}>{v.units ?? 0} units</span>
                    <span className="block text-xs text-muted-foreground">
                      {v.batches ?? 0} batch{v.batches === 1 ? '' : 'es'} · cost {formatCurrency(v.costValue ?? 0)} · MRP {formatCurrency(v.mrpValue ?? 0)}
                    </span>
                  </button>
                );
              })}
            </div>
            <p className="text-xs text-muted-foreground">
              Today is {data.today} (shop time). {section ? 'Showing one section; select it again to show all.' : 'Select a box to show only that section.'}
            </p>
            <div className="overflow-x-auto rounded-lg border">
              <table className="w-full text-sm">
                <thead className="bg-muted text-left text-xs font-semibold text-muted-foreground">
                  <tr>
                    <th scope="col" className="px-3 py-2">
                      Item
                    </th>
                    <th scope="col" className="px-3 py-2">
                      Batch
                    </th>
                    <th scope="col" className="px-3 py-2">
                      Expiry
                    </th>
                    <th scope="col" className="px-3 py-2">
                      Store
                    </th>
                    <th scope="col" className="px-3 py-2 text-right">
                      Qty
                    </th>
                    <th scope="col" className="px-3 py-2 text-right">
                      Cost value
                    </th>
                    <th scope="col" className="px-3 py-2 text-right">
                      MRP value
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {data.items.length === 0 && (
                    <tr>
                      <td colSpan={7} className="px-3 py-6 text-center text-muted-foreground">
                        Nothing to act on.
                      </td>
                    </tr>
                  )}
                  {data.items.map((x) => {
                    const sec = SECTIONS.find((s) => s.key === x.section);
                    return (
                      <tr key={`${x.section}-${x.batchId}-${x.storeId}`}>
                        <td className="px-3 py-2">
                          <div className="font-medium">{x.productName}</div>
                          <div className="text-xs text-muted-foreground">
                            {x.sku} · <span className={sec?.tone}>{sec?.label}</span>
                          </div>
                        </td>
                        <td className="px-3 py-2">
                          {x.unknown ? (
                            'No batch'
                          ) : (
                            <button type="button" className="font-medium text-primary underline-offset-2 hover:underline" onClick={() => setOpen(x.batchId)}>
                              {x.batchNo}
                            </button>
                          )}
                        </td>
                        <td className="px-3 py-2 tabular-nums">
                          {x.expiry || '—'}
                          {x.daysToExpiry != null && x.section === 'expiring' && <div className="text-xs text-muted-foreground">in {x.daysToExpiry} days</div>}
                        </td>
                        <td className="px-3 py-2">{x.storeName}</td>
                        <td className="px-3 py-2 text-right tabular-nums">{x.quantity}</td>
                        <td className="px-3 py-2 text-right tabular-nums">{formatCurrency(x.costValue)}</td>
                        <td className="px-3 py-2 text-right tabular-nums">{formatCurrency(x.mrpValue)}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </>
        )}
      </div>
      {open && (
        <BatchDialog
          batchId={open}
          canManage={hasPermission('batches', 'manage')}
          canWriteOff={hasPermission('batches', 'write_off')}
          canRecall={hasPermission('batches', 'recall')}
          onClose={() => setOpen(null)}
          onSaved={() => setReload((n) => n + 1)}
        />
      )}
    </ListPageShell>
  );
}
