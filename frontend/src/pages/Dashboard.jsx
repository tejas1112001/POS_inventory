import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import {
  AlertTriangle,
  Ban,
  Banknote,
  CalendarClock,
  CalendarX,
  CreditCard,
  Hand,
  HandCoins,
  Package,
  PackageX,
  Percent,
  ReceiptText,
  ShieldAlert,
  ShoppingCart,
  Truck,
  Undo2,
  Users,
  Wallet,
} from 'lucide-react';
import { Bar, Doughnut, Line } from 'react-chartjs-2';
import { ArcElement, BarElement, CategoryScale, Chart as ChartJS, Filler, LinearScale, LineElement, PointElement, Tooltip } from 'chart.js';
import { Button } from '@/components/ui/button';
import { api } from '@/services/api';
import { usePos } from '@/hooks/usePos';
import { usePermission } from '@/context/PermissionContext';
import { useTheme } from '@/context/ThemeContext';
import { ErrorState } from '@/components/kit/States';
import StatCard from '@/components/kit/StatCard';
import MetricCard from '@/components/kit/MetricCard';
import ActionCard from '@/components/kit/ActionCard';
import Panel, { ViewAllLink } from '@/components/kit/Panel';
import SegmentedControl from '@/components/kit/SegmentedControl';
import { LowStockList, RecentSalesTable } from '@/components/dashboard/DashboardLists';
import { formatCurrency, formatNumber, getFormatSettings, todayInShop } from '@/lib/format';
import { cn } from '@/lib/utils';
import OnboardingCard from '@/components/dashboard/OnboardingCard';
import { fmtQty } from '@/lib/qty';

ChartJS.register(CategoryScale, LinearScale, BarElement, LineElement, PointElement, ArcElement, Filler, Tooltip);

// Teal & Cream chart palette (moodboard): fresh teal, deep teal, amber,
// mint, then softer tints. Each slice also has a legend row with its share,
// so colour is never the only cue.
const SLICES_LIGHT = ['#20b6a4', '#123b45', '#f2b544', '#8fd9cd', '#17807a', '#f6d48f', '#5f7377', '#c9ece6'];
const SLICES_DARK = ['#20b6a4', '#8fd9cd', '#f2b544', '#2b8f87', '#c9ece6', '#f6d48f', '#9db3b3', '#17807a'];

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

function Skeleton() {
  return (
    <div className="space-y-4" aria-busy="true" aria-label="Loading the dashboard">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="h-[104px] animate-pulse rounded-2xl bg-muted" />
        ))}
      </div>
      <div className="grid gap-3 xl:grid-cols-3">
        <div className="h-80 animate-pulse rounded-2xl bg-muted xl:col-span-2" />
        <div className="h-80 animate-pulse rounded-2xl bg-muted" />
      </div>
    </div>
  );
}

// Dashboard (Teal & Cream): greeting, store and date range, the headline
// figures with the change against the previous period, money, the revenue
// trend, top products, recent orders, low stock and action cards. Cashiers
// see their own sales only (server).
export default function Dashboard() {
  const { currentUser, tenant } = usePos();
  const { hasPermission } = usePermission();
  const { mode } = useTheme();
  const dark = mode === 'dark';
  const [params, setParams] = useSearchParams();
  const [stores, setStores] = useState([]);
  const [data, setData] = useState(null);
  const [recent, setRecent] = useState(null);
  const [low, setLow] = useState(null);
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

  // The two short lists load next to the figures (5 rows each, in parallel).
  const canSales = hasPermission('sales', 'view');
  const canStock = hasPermission('inventory', 'view');
  useEffect(() => {
    let active = true;
    const store = storeId ? `&storeId=${encodeURIComponent(storeId)}` : '';
    const rowsOf = (res) => (Array.isArray(res) ? res : res?.data || []);
    if (canSales) {
      api
        .get(`/api/sales?page=1&limit=5${store}`, { quiet: true })
        .then((r) => active && setRecent(rowsOf(r)))
        .catch(() => active && setRecent([]));
    }
    if (canStock) {
      api
        .get(`/api/inventory?page=1&limit=5&stockStatus=${encodeURIComponent('Low Stock')}${store}`, { quiet: true })
        .then((r) => active && setLow(rowsOf(r)))
        .catch(() => active && setLow([]));
    }
    return () => {
      active = false;
    };
  }, [canSales, canStock, storeId, tick]);

  const firstName = (currentUser?.name || '').split(' ')[0];
  const x = data?.extra;
  const cur = x?.current;
  const prev = x?.previous;
  const ownOnly = data?.filters?.ownOnly;
  const avg = (f) => (f && f.orders ? Number(f.revenue) / f.orders : 0);
  const series = x?.series || [];
  const top = data?.salesDistribution || [];
  const topTotal = top.reduce((s, t) => s + (Number(t.value) || 0), 0);
  const rangeLabel = RANGES.find((r) => r.key === rangeKey)?.label;
  const slices = dark ? SLICES_DARK : SLICES_LIGHT;

  const tick_ = dark ? '#9db3b3' : '#566b70';
  const grid = dark ? 'rgba(157, 179, 179, 0.14)' : '#ece9df';
  const line = '#20b6a4';
  const tooltip = {
    backgroundColor: dark ? '#0b2c35' : '#123b45',
    titleColor: '#ffffff',
    bodyColor: '#e6f7f3',
    padding: 10,
    cornerRadius: 10,
    displayColors: false,
  };

  const chartData = {
    labels: series.map((s) => s.label),
    datasets: [
      {
        label: metric === 'revenue' ? 'Sales' : 'Orders',
        data: series.map((s) => Number(metric === 'revenue' ? s.revenue : s.orders)),
        borderColor: line,
        borderWidth: 2.5,
        backgroundColor:
          x?.granularity === 'month'
            ? line
            : (ctx) => {
                const { chart } = ctx;
                const area = chart.chartArea;
                if (!area) return 'rgba(32, 182, 164, 0.15)';
                const g = chart.ctx.createLinearGradient(0, area.top, 0, area.bottom);
                g.addColorStop(0, dark ? 'rgba(32, 182, 164, 0.35)' : 'rgba(32, 182, 164, 0.28)');
                g.addColorStop(1, 'rgba(32, 182, 164, 0)');
                return g;
              },
        fill: x?.granularity !== 'month',
        tension: 0.38,
        pointRadius: (ctx) => (ctx.dataIndex === series.length - 1 ? 5 : series.length > 31 ? 0 : 2.5),
        pointBackgroundColor: dark ? '#10353f' : '#ffffff',
        pointBorderColor: line,
        pointBorderWidth: 2,
        pointHoverRadius: 6,
        borderRadius: 6,
      },
    ],
  };
  const chartOptions = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: 'index', intersect: false },
    plugins: {
      legend: { display: false },
      tooltip: { ...tooltip, callbacks: { label: (c) => (metric === 'revenue' ? formatCurrency(c.parsed.y) : `${c.parsed.y} orders`) } },
    },
    scales: {
      x: { grid: { display: false }, border: { display: false }, ticks: { color: tick_, maxRotation: 0, autoSkip: true, maxTicksLimit: 12, font: { family: 'Manrope Variable', size: 11 } } },
      y: {
        beginAtZero: true,
        grid: { color: grid },
        border: { display: false },
        ticks: { color: tick_, precision: 0, font: { family: 'Manrope Variable', size: 11 }, callback: (v) => (metric === 'revenue' ? formatNumber(v, 0) : v) },
      },
    },
  };
  const topData = {
    labels: top.map((t) => t.category),
    datasets: [
      {
        data: top.map((t) => Number(t.value)),
        backgroundColor: top.map((_, i) => slices[i % slices.length]),
        borderColor: dark ? '#10353f' : '#ffffff',
        borderWidth: 3,
        hoverOffset: 4,
      },
    ],
  };
  const topOptions = {
    responsive: true,
    maintainAspectRatio: false,
    cutout: '70%',
    plugins: { legend: { display: false }, tooltip: { ...tooltip, displayColors: true, callbacks: { label: (c) => ` ${formatCurrency(c.parsed)}` } } },
  };

  const actions = [];
  if (x?.lowStock != null && (x.lowStock > 0 || x.outOfStock > 0)) {
    actions.push(<ActionCard key="low" icon={AlertTriangle} tone="warn" title="Low stock" value={x.lowStock} text="items running low" to={`/inventory?stockStatus=${encodeURIComponent('Low Stock')}`} />);
    if (x.outOfStock > 0) actions.push(<ActionCard key="out" icon={PackageX} tone="danger" title="Out of stock" value={x.outOfStock} text="items at zero" to={`/inventory?stockStatus=${encodeURIComponent('Out of Stock')}`} />);
  }
  // Pharmacy (industry P3 item 7): batch figures, in units; each opens the expiry report.
  const bx = data?.batches;
  if (bx && hasPermission('reports', 'view')) {
    if (bx.expired > 0) actions.push(<ActionCard key="bx-exp" icon={CalendarX} tone="danger" title="Expired on hand" value={bx.expired} text="units to remove" to="/reports/expiry?section=expired" />);
    if (bx.expiring > 0) actions.push(<ActionCard key="bx-soon" icon={CalendarClock} tone="warn" title="Expiring soon" value={bx.expiring} text={`units within ${bx.windowDays} days`} to="/reports/expiry?section=expiring" />);
    if (bx.recalled > 0) actions.push(<ActionCard key="bx-rec" icon={Ban} tone="danger" title="Recalled" value={bx.recalled} text="units still held" to="/reports/expiry?section=recalled" />);
    if (bx.quarantined > 0) actions.push(<ActionCard key="bx-q" icon={ShieldAlert} title="Quarantined" value={bx.quarantined} text="units set aside" to="/reports/expiry?section=quarantined" />);
  }
  if (bx && bx.unbatched > 0) actions.push(<ActionCard key="bx-unb" icon={AlertTriangle} tone="warn" title="No batch" value={bx.unbatched} text="units without a batch number" to="/batches?status=unknown" />);
  if (x?.purchasesWaiting) actions.push(<ActionCard key="pur" icon={Truck} title="Purchases to receive" value={x.purchasesWaiting} text="ordered or pending" to="/purchases" />);
  if (x?.customersWithDues) actions.push(<ActionCard key="due" icon={Users} tone="warn" title="Customers with dues" value={x.customersWithDues} text="owe money" to="/customer-due-report" />);
  if (hasPermission('billing', 'view') && tenant?.status && tenant.status !== 'active') {
    actions.push(<ActionCard key="bill" icon={CreditCard} tone={tenant.status === 'trialing' ? 'info' : 'danger'} title="Plan & billing" text={`Shop status: ${tenant.status.replace('_', ' ')}`} to="/billing" />);
  }

  return (
    <div className="space-y-3 sm:space-y-4">
      <div className="flex flex-col gap-3 xl:flex-row xl:items-end xl:justify-between">
        <div>
          <h2 className="flex items-center gap-2 text-2xl font-extrabold tracking-tight">
            {greeting()}
            {firstName ? `, ${firstName}` : ''}
            <Hand className="size-6 -rotate-12 text-amber-brand" aria-hidden="true" />
          </h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            {ownOnly ? 'Here is how your own sales are doing.' : 'Here’s what’s happening at your store.'}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {stores.length > 1 && (
            <select aria-label="Store" className="h-10 rounded-lg border border-input bg-card px-3 text-sm font-medium shadow-xs" value={storeId} onChange={(e) => setParam({ store: e.target.value })}>
              <option value="">All stores</option>
              {stores.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          )}
          <SegmentedControl
            label="Date range"
            options={RANGES.map((r) => ({ value: r.key, label: r.label }))}
            value={rangeKey}
            onChange={(k) => setParam(k === 'custom' ? { range: 'custom', from, to } : { range: k === 'today' ? '' : k, from: '', to: '' })}
          />
          {rangeKey === 'custom' && (
            <div className="flex items-center gap-1.5 text-sm">
              <input type="date" aria-label="From" className="h-10 rounded-lg border border-input bg-card px-2 shadow-xs" value={from} max={to} onChange={(e) => e.target.value && setParam({ from: e.target.value })} />
              <span className="text-muted-foreground">to</span>
              <input type="date" aria-label="To" className="h-10 rounded-lg border border-input bg-card px-2 shadow-xs" value={to} min={from} max={today} onChange={(e) => e.target.value && setParam({ to: e.target.value })} />
            </div>
          )}
        </div>
      </div>

      {error ? (
        <div className="rounded-2xl border bg-card">
          <ErrorState message={error} onRetry={() => setTick((n) => n + 1)} />
        </div>
      ) : loading && !data ? (
        <Skeleton />
      ) : (
        cur && (
          <div className={cn('space-y-3 sm:space-y-4 transition-opacity', loading && 'opacity-60')} aria-busy={loading || undefined}>
            {cur.orders === 0 && rangeKey === 'today' && hasPermission('pos', 'view') && (
              <div className="relative flex flex-wrap items-center justify-between gap-3 overflow-hidden rounded-2xl bg-brand-gradient p-4 text-white shadow-sm">
                <div>
                  <p className="text-base font-bold">No sales yet today</p>
                  <p className="text-sm text-white/85">Open the checkout to ring up the first bill.</p>
                </div>
                <Button asChild className="bg-white bg-none text-[#123b45] shadow-sm hover:bg-white hover:brightness-95">
                  <Link to="/pos">
                    <ShoppingCart className="size-4" aria-hidden="true" /> Open checkout
                  </Link>
                </Button>
              </div>
            )}

            <OnboardingCard />

            <section aria-label={`${rangeLabel} figures`} className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
              <StatCard icon={Wallet} label="Net sales" value={formatCurrency(cur.net)} delta={prev && change(cur.net, prev.net)} hint={prev ? 'vs previous period' : 'after returns'} />
              <StatCard icon={ShoppingCart} label="Orders" value={formatNumber(cur.orders, 0)} delta={prev && change(cur.orders, prev.orders)} />
              <StatCard icon={ReceiptText} tone="deep" label="Average bill" value={formatCurrency(avg(cur))} delta={prev && change(avg(cur), avg(prev))} />
              <StatCard icon={Package} tone="amber" label="Items sold" value={fmtQty(cur.itemsSold)} delta={prev && change(cur.itemsSold, prev.itemsSold)} />
            </section>

            <section aria-label="Money" className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
              <MetricCard icon={Banknote} label="Collected" value={formatCurrency(cur.collected ?? cur.paid)} hint="Payments received, incl. dues" />
              {!ownOnly && <MetricCard icon={HandCoins} label="Due from customers" value={formatCurrency(cur.due)} hint={hasPermission('reports', 'view') ? 'See customer dues →' : undefined} to={hasPermission('reports', 'view') ? '/customer-due-report' : undefined} />}
              <MetricCard icon={Undo2} label="Refunds" value={formatCurrency(cur.returns)} />
              <MetricCard icon={Percent} label="Discounts given" value={formatCurrency(cur.discounts)} />
            </section>

            {actions.length > 0 && (
              <section aria-label="Needs attention" className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                {actions}
              </section>
            )}

            <div className="grid gap-3 xl:grid-cols-3">
              <Panel
                id="sales-chart"
                title={metric === 'revenue' ? 'Revenue trend' : 'Orders trend'}
                description={`By ${x.granularity} · ${rangeLabel}`}
                className="xl:col-span-2"
                action={
                  <SegmentedControl
                    label="Chart shows"
                    options={[
                      { value: 'revenue', label: 'Revenue' },
                      { value: 'orders', label: 'Orders' },
                    ]}
                    value={metric}
                    onChange={setMetric}
                  />
                }
              >
                <div className="h-64 sm:h-72">
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
              </Panel>

              <Panel id="top-products" title="Top products">
                {top.length ? (
                  <div className="flex flex-1 flex-col items-center gap-4 sm:flex-row xl:flex-col 2xl:flex-row">
                    <div className="relative size-44 shrink-0">
                      <Doughnut data={topData} options={topOptions} aria-label="Top products by sales" role="img" />
                      <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center text-center">
                        <span className="text-base font-extrabold text-navy tabular-nums">{formatCurrency(topTotal)}</span>
                        <span className="text-[11px] font-medium text-muted-foreground">Top sales</span>
                      </div>
                    </div>
                    <ul className="w-full min-w-0 flex-1 space-y-2" aria-label="Share of top products">
                      {top.map((t, i) => (
                        <li key={`${t.category}-${i}`} className="flex items-center gap-2 text-sm">
                          <span className="size-2.5 shrink-0 rounded-full" style={{ background: slices[i % slices.length] }} aria-hidden="true" />
                          <span className="min-w-0 flex-1 truncate">{t.category}</span>
                          <span className="font-semibold text-navy tabular-nums">{topTotal ? `${((Number(t.value) / topTotal) * 100).toFixed(1)}%` : '—'}</span>
                        </li>
                      ))}
                    </ul>
                  </div>
                ) : (
                  <p className="flex h-64 items-center justify-center text-sm text-muted-foreground">No sales in this period.</p>
                )}
              </Panel>
            </div>

            {(canSales || canStock) && (
              <div className="grid gap-3 xl:grid-cols-3">
                {canSales && (
                  <Panel id="recent-sales" title="Recent transactions" className={cn(canStock ? 'xl:col-span-2' : 'xl:col-span-3')} action={<ViewAllLink to="/sales" label="View all orders" />}>
                    {recent == null ? <div className="h-48 animate-pulse rounded-xl bg-muted" /> : <RecentSalesTable rows={recent} />}
                  </Panel>
                )}
                {canStock && (
                  <Panel id="low-stock" title="Low stock alerts" className={cn(!canSales && 'xl:col-span-3')} action={<ViewAllLink to={`/inventory?stockStatus=${encodeURIComponent('Low Stock')}`} label="View all low stock" />}>
                    {low == null ? <div className="h-48 animate-pulse rounded-xl bg-muted" /> : <LowStockList rows={low} />}
                  </Panel>
                )}
              </div>
            )}
          </div>
        )
      )}
    </div>
  );
}
