import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { AlertTriangle, ArrowDownRight, ArrowUpRight, CreditCard, PackageX, ShoppingCart, Truck, Users } from 'lucide-react';
import { Bar, Line } from 'react-chartjs-2';
import { BarElement, CategoryScale, Chart as ChartJS, Filler, LinearScale, LineElement, PointElement, Tooltip } from 'chart.js';
import { Button } from '@/components/ui/button';
import { api } from '@/services/api';
import { usePos } from '@/hooks/usePos';
import { usePermission } from '@/context/PermissionContext';
import { ErrorState } from '@/components/kit/States';
import { formatCurrency, formatNumber, getFormatSettings, todayInShop } from '@/lib/format';
import { cn } from '@/lib/utils';
import OnboardingCard from '@/components/dashboard/OnboardingCard';

ChartJS.register(CategoryScale, LinearScale, BarElement, LineElement, PointElement, Filler, Tooltip);

const BLUE = '#2563eb';
const NAVY = '#0f172a';
const GRID = '#e2e8f0';

// Dates are YYYY-MM-DD in the shop's time zone.
const addDays = (d, n) => {
  const [y, m, day] = d.split('-').map(Number);
  return new Date(Date.UTC(y, m - 1, day + n)).toISOString().slice(0, 10);
};
const RANGES = [
  { key: 'today', label: 'Today', make: (t) => [t, t] },
  { key: 'yesterday', label: 'Yesterday', make: (t) => [addDays(t, -1), addDays(t, -1)] },
  { key: '7d', label: 'Last 7 days', make: (t) => [addDays(t, -6), t] },
  { key: 'month', label: 'This month', make: (t) => [`${t.slice(0, 8)}01`, t] },
  { key: 'custom', label: 'Custom' },
];

function greeting() {
  let hour = new Date().getHours();
  try {
    hour = Number(new Intl.DateTimeFormat('en-GB', { hour: 'numeric', hourCycle: 'h23', timeZone: getFormatSettings().timezone || undefined }).format(new Date()));
  } catch {
    /* browser time */
  }
  return hour < 12 ? 'Good morning' : hour < 17 ? 'Good afternoon' : 'Good evening';
}

// change: "+12%" vs the previous period; null when there is nothing to compare.
function change(now, before) {
  const a = Number(now) || 0;
  const b = Number(before) || 0;
  if (!b) return a ? { text: 'new', up: true } : null;
  const pct = ((a - b) / Math.abs(b)) * 100;
  return { text: `${pct >= 0 ? '+' : ''}${pct.toFixed(Math.abs(pct) < 10 ? 1 : 0)}%`, up: pct >= 0 };
}

function Stat({ label, value, delta, hint, to, big }) {
  const body = (
    <div className={cn('flex h-full flex-col gap-1 rounded-lg border bg-card p-4 shadow-xs', to && 'hover:border-primary/40')}>
      <p className="text-sm text-muted-foreground">{label}</p>
      <p className={cn('font-bold tabular-nums text-navy', big ? 'text-2xl' : 'text-xl')}>{value}</p>
      <div className="mt-auto flex items-center gap-2 text-xs">
        {delta && (
          <span className={cn('inline-flex items-center gap-0.5 font-medium', delta.up ? 'text-success-fg' : 'text-danger-fg')}>
            {delta.up ? <ArrowUpRight className="size-3.5" aria-hidden="true" /> : <ArrowDownRight className="size-3.5" aria-hidden="true" />}
            {delta.text}
          </span>
        )}
        {hint && <span className="text-muted-foreground">{hint}</span>}
      </div>
    </div>
  );
  return to ? (
    <Link to={to} className="block rounded-lg focus-visible:outline-2 focus-visible:outline-ring">
      {body}
    </Link>
  ) : (
    body
  );
}

function ActionCard({ icon: Icon, tone = 'info', title, value, text, to }) {
  const tones = { info: 'bg-accent text-accent-foreground', warn: 'bg-warning-bg text-warning-fg', danger: 'bg-danger-bg text-danger-fg' };
  return (
    <Link to={to} className="flex items-center gap-3 rounded-lg border bg-card p-3 shadow-xs hover:border-primary/40 focus-visible:outline-2 focus-visible:outline-ring">
      <span className={cn('flex size-10 shrink-0 items-center justify-center rounded-full', tones[tone])}>
        <Icon className="size-5" aria-hidden="true" />
      </span>
      <span className="min-w-0">
        <span className="block text-sm font-medium">{title}</span>
        <span className="block truncate text-xs text-muted-foreground">
          {value != null && <span className="font-semibold text-foreground tabular-nums">{formatNumber(value, 0)} </span>}
          {text}
        </span>
      </span>
    </Link>
  );
}

function Skeleton() {
  return (
    <div className="space-y-4" aria-busy="true" aria-label="Loading the dashboard">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="h-28 animate-pulse rounded-lg bg-muted" />
        ))}
      </div>
      <div className="h-72 animate-pulse rounded-lg bg-muted" />
    </div>
  );
}

// Dashboard (UI S7): greeting, store and date range, today's figures with
// the change against the previous period, money, the sales chart, top
// products and action cards. Cashiers see their own sales only (server).
export default function Dashboard() {
  const { currentUser, tenant } = usePos();
  const { hasPermission } = usePermission();
  const [params, setParams] = useSearchParams();
  const [stores, setStores] = useState([]);
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [metric, setMetric] = useState('revenue');
  const [tick, setTick] = useState(0);

  const today = todayInShop();
  const rangeKey = RANGES.some((r) => r.key === params.get('range')) ? params.get('range') : 'today';
  const [from, to] = useMemo(() => {
    if (rangeKey === 'custom') return [params.get('from') || today, params.get('to') || today];
    return RANGES.find((r) => r.key === rangeKey).make(today);
  }, [rangeKey, params, today]);
  const storeId = params.get('store') || '';

  const setParam = useCallback(
    (patch) =>
      setParams(
        () => {
          const next = new URLSearchParams(window.location.search);
          for (const [k, v] of Object.entries(patch)) v ? next.set(k, v) : next.delete(k);
          return next;
        },
        { replace: true }
      ),
    [setParams]
  );

  useEffect(() => {
    if (!hasPermission('stores', 'view')) return;
    api
      .get('/api/stores', { quiet: true })
      .then((s) => setStores(Array.isArray(s) ? s : s?.data || []))
      .catch(() => {});
  }, [hasPermission]);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError('');
    const q = new URLSearchParams({ startDate: from, endDate: to });
    if (storeId) q.set('storeId', storeId);
    api
      .get(`/api/dashboard?${q}`, { quiet: true })
      .then((d) => active && setData(d))
      .catch((err) => active && setError(err.message || 'The dashboard could not load.'))
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
    };
  }, [from, to, storeId, tick]);

  const firstName = (currentUser?.name || '').split(' ')[0];
  const x = data?.extra;
  const cur = x?.current;
  const prev = x?.previous;
  const ownOnly = data?.filters?.ownOnly;
  const avg = (f) => (f && f.orders ? Number(f.revenue) / f.orders : 0);
  const series = x?.series || [];
  const top = data?.salesDistribution || [];
  const rangeLabel = RANGES.find((r) => r.key === rangeKey)?.label;

  const chartData = {
    labels: series.map((s) => s.label),
    datasets: [
      {
        label: metric === 'revenue' ? 'Sales' : 'Orders',
        data: series.map((s) => Number(metric === 'revenue' ? s.revenue : s.orders)),
        borderColor: BLUE,
        backgroundColor: x?.granularity === 'month' ? BLUE : 'rgba(37, 99, 235, 0.12)',
        fill: x?.granularity !== 'month',
        tension: 0.3,
        pointRadius: series.length > 31 ? 0 : 3,
        borderRadius: 4,
      },
    ],
  };
  const chartOptions = {
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: { display: false },
      tooltip: { callbacks: { label: (c) => (metric === 'revenue' ? formatCurrency(c.parsed.y) : `${c.parsed.y} orders`) } },
    },
    scales: {
      x: { grid: { display: false }, ticks: { color: '#64748b', maxRotation: 0, autoSkip: true, maxTicksLimit: 12 } },
      y: { beginAtZero: true, grid: { color: GRID }, ticks: { color: '#64748b', precision: 0, callback: (v) => (metric === 'revenue' ? formatNumber(v, 0) : v) } },
    },
  };
  const topData = {
    labels: top.map((t) => t.category),
    datasets: [{ data: top.map((t) => Number(t.value)), backgroundColor: NAVY, borderRadius: 4, barThickness: 18 }],
  };
  const topOptions = {
    indexAxis: 'y',
    responsive: true,
    maintainAspectRatio: false,
    plugins: { legend: { display: false }, tooltip: { callbacks: { label: (c) => formatCurrency(c.parsed.x) } } },
    scales: { x: { beginAtZero: true, grid: { color: GRID }, ticks: { color: '#64748b', callback: (v) => formatNumber(v, 0) } }, y: { grid: { display: false }, ticks: { color: '#334155' } } },
  };

  const actions = [];
  if (x?.lowStock != null && (x.lowStock > 0 || x.outOfStock > 0)) {
    actions.push(<ActionCard key="low" icon={AlertTriangle} tone="warn" title="Low stock" value={x.lowStock} text="items running low" to={`/inventory?stockStatus=${encodeURIComponent('Low Stock')}`} />);
    if (x.outOfStock > 0) actions.push(<ActionCard key="out" icon={PackageX} tone="danger" title="Out of stock" value={x.outOfStock} text="items at zero" to={`/inventory?stockStatus=${encodeURIComponent('Out of Stock')}`} />);
  }
  if (x?.purchasesWaiting) actions.push(<ActionCard key="pur" icon={Truck} title="Purchases to receive" value={x.purchasesWaiting} text="ordered or pending" to="/purchases" />);
  if (x?.customersWithDues) actions.push(<ActionCard key="due" icon={Users} tone="warn" title="Customers with dues" value={x.customersWithDues} text="owe money" to="/customer-due-report" />);
  if (hasPermission('billing', 'view') && tenant?.status && tenant.status !== 'active') {
    actions.push(<ActionCard key="bill" icon={CreditCard} tone={tenant.status === 'trialing' ? 'info' : 'danger'} title="Plan & billing" text={`Shop status: ${tenant.status.replace('_', ' ')}`} to="/billing" />);
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <h2 className="text-xl font-semibold">
            {greeting()}
            {firstName ? `, ${firstName}` : ''}
          </h2>
          <p className="text-sm text-muted-foreground">{ownOnly ? 'Your own sales.' : 'Sales of the whole shop.'}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {stores.length > 1 && (
            <select aria-label="Store" className="h-10 rounded-md border bg-card px-2 text-sm" value={storeId} onChange={(e) => setParam({ store: e.target.value })}>
              <option value="">All stores</option>
              {stores.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          )}
          <div className="no-scrollbar flex gap-1 overflow-x-auto rounded-lg bg-muted p-1" role="group" aria-label="Date range">
            {RANGES.map((r) => (
              <button
                key={r.key}
                type="button"
                aria-pressed={rangeKey === r.key}
                onClick={() => setParam(r.key === 'custom' ? { range: 'custom', from, to } : { range: r.key === 'today' ? '' : r.key, from: '', to: '' })}
                className={cn('min-h-9 shrink-0 rounded-md px-3 text-sm font-medium', rangeKey === r.key ? 'bg-card text-foreground shadow-xs' : 'text-slate-600 hover:text-foreground dark:text-slate-300')}
              >
                {r.label}
              </button>
            ))}
          </div>
          {rangeKey === 'custom' && (
            <div className="flex items-center gap-1 text-sm">
              <input type="date" aria-label="From" className="h-10 rounded-md border bg-card px-2" value={from} max={to} onChange={(e) => e.target.value && setParam({ from: e.target.value })} />
              <span className="text-muted-foreground">to</span>
              <input type="date" aria-label="To" className="h-10 rounded-md border bg-card px-2" value={to} min={from} max={today} onChange={(e) => e.target.value && setParam({ to: e.target.value })} />
            </div>
          )}
        </div>
      </div>

      {error ? (
        <ErrorState message={error} onRetry={() => setTick((n) => n + 1)} />
      ) : loading && !data ? (
        <Skeleton />
      ) : (
        cur && (
          <div className={cn('space-y-4 transition-opacity', loading && 'opacity-60')} aria-busy={loading || undefined}>
            {cur.orders === 0 && rangeKey === 'today' && hasPermission('pos', 'view') && (
              <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-dashed bg-card p-4">
                <p className="text-sm text-muted-foreground">No sales yet today.</p>
                <Button asChild>
                  <Link to="/pos">
                    <ShoppingCart className="size-4" aria-hidden="true" /> Open Checkout
                  </Link>
                </Button>
              </div>
            )}

            <OnboardingCard />

            <section aria-label={`${rangeLabel} figures`} className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <Stat big label="Net sales" value={formatCurrency(cur.net)} delta={prev && change(cur.net, prev.net)} hint={prev ? 'vs previous period' : 'after returns'} />
              <Stat big label="Orders" value={formatNumber(cur.orders, 0)} delta={prev && change(cur.orders, prev.orders)} />
              <Stat big label="Average bill" value={formatCurrency(avg(cur))} delta={prev && change(avg(cur), avg(prev))} />
              <Stat big label="Items sold" value={formatNumber(cur.itemsSold, 0)} delta={prev && change(cur.itemsSold, prev.itemsSold)} />
            </section>

            <section aria-label="Money" className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <Stat label="Collected" value={formatCurrency(cur.collected ?? cur.paid)} hint="Payments received, incl. dues" />
              {!ownOnly && <Stat label="Due from customers" value={formatCurrency(cur.due)} hint={hasPermission('reports', 'view') ? 'See customer dues →' : undefined} to={hasPermission('reports', 'view') ? '/customer-due-report' : undefined} />}
              <Stat label="Refunds" value={formatCurrency(cur.returns)} />
              <Stat label="Discounts given" value={formatCurrency(cur.discounts)} />
            </section>

            {actions.length > 0 && <section aria-label="Needs attention" className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">{actions}</section>}

            <div className="grid gap-4 xl:grid-cols-3">
              <section className="rounded-lg border bg-card p-4 shadow-xs xl:col-span-2" aria-labelledby="sales-chart">
                <div className="mb-3 flex items-center justify-between gap-2">
                  <h2 id="sales-chart" className="text-base font-semibold">
                    Sales by {x.granularity}
                  </h2>
                  <div className="flex rounded-md bg-muted p-0.5" role="group" aria-label="Chart shows">
                    {[
                      ['revenue', 'Revenue'],
                      ['orders', 'Orders'],
                    ].map(([k, l]) => (
                      <button key={k} type="button" aria-pressed={metric === k} onClick={() => setMetric(k)} className={cn('min-h-8 rounded px-3 text-sm', metric === k ? 'bg-card font-medium shadow-xs' : 'text-slate-600 dark:text-slate-300')}>
                        {l}
                      </button>
                    ))}
                  </div>
                </div>
                <div className="h-64">
                  {series.length ? (
                    x.granularity === 'month' ? (
                      <Bar data={chartData} options={chartOptions} aria-label="Sales chart" role="img" />
                    ) : (
                      <Line data={chartData} options={chartOptions} aria-label="Sales chart" role="img" />
                    )
                  ) : (
                    <p className="flex h-full items-center justify-center text-sm text-muted-foreground">No sales in this period.</p>
                  )}
                </div>
              </section>
              <section className="rounded-lg border bg-card p-4 shadow-xs" aria-labelledby="top-products">
                <h2 id="top-products" className="mb-3 text-base font-semibold">
                  Top products
                </h2>
                <div className="h-64">
                  {top.length ? (
                    <Bar data={topData} options={topOptions} aria-label="Top products by sales" role="img" />
                  ) : (
                    <p className="flex h-full items-center justify-center text-sm text-muted-foreground">No sales in this period.</p>
                  )}
                </div>
              </section>
            </div>
          </div>
        )
      )}
    </div>
  );
}
