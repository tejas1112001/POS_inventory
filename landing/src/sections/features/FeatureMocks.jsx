import { useState } from 'react';
import { CirclePause, Download, Lock, Ruler, Search, Shirt } from 'lucide-react';
import { sellTiles } from '@/content/site';
import { cn } from '@/lib/cn';
import { money, money0 } from '@/lib/format';
import { Button } from '@/components/ui/Button';
import { Segmented } from '@/components/ui/Segmented';

const Hint = ({ children }) => <p className="mt-2.5 text-[13px] text-muted">{children}</p>;
const th = 'py-2 text-xs font-bold tracking-[0.06em] text-muted uppercase';

export function SellingMock() {
  const [bill, setBill] = useState({ count: 0, total: 0 });
  return (
    <div>
      <div className="flex h-[46px] items-center gap-2.5 rounded-xl border border-slate-300 px-3.5 text-[15px] text-muted">
        <Search className="size-[18px]" aria-hidden="true" />
        Search a product or scan a barcode
      </div>
      <div className="mt-4 grid grid-cols-[repeat(auto-fit,minmax(160px,1fr))] gap-3">
        {sellTiles.map((t) => (
          <button
            key={t.name}
            type="button"
            onClick={() => setBill((b) => ({ count: b.count + 1, total: b.total + t.price }))}
            aria-label={`Add ${t.name} to bill`}
            className="card-lift flex min-h-[104px] flex-col gap-1.5 rounded-[14px] border border-line bg-white p-3.5 text-left"
          >
            <span className="flex items-start justify-between gap-2">
              <span className="text-[15px] font-bold text-ink">{t.name}</span>
              <t.icon className="size-4 shrink-0 text-slate-400" aria-hidden="true" />
            </span>
            <span className="text-[13px] text-muted">{t.sizes}</span>
            <span className="mt-auto text-[15px] font-extrabold text-brand-strong">{money0(t.price)}</span>
          </button>
        ))}
      </div>
      <div className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-[14px] bg-ink px-[18px] py-3.5 text-slate-200">
        <span className="text-[15px]" aria-live="polite">
          Bill: <strong className="text-white">{bill.count} items</strong> ·{' '}
          <strong className="text-white">{money(bill.total)}</strong>
        </span>
        <span className="flex flex-wrap gap-2">
          <span className="inline-flex items-center gap-1.5 rounded-full bg-amber-500/20 px-3 py-1.5 text-[13px] font-semibold text-amber-300">
            <CirclePause className="size-3.5" aria-hidden="true" />2 bills on hold
          </span>
          <button
            type="button"
            onClick={() => setBill({ count: 0, total: 0 })}
            className="min-h-9 rounded-full border border-slate-700 px-3.5 text-[13px] text-slate-300 hover:text-white"
          >
            Clear
          </button>
        </span>
      </div>
      <Hint>Try it: tap a product to add it to the bill.</Hint>
    </div>
  );
}

export function GstMock() {
  const [inter, setInter] = useState(false);
  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-lg font-extrabold text-ink">Tax invoice</p>
          <p className="text-[13px] text-muted">INV/2026-27/000184 · GSTIN [SHOP GSTIN]</p>
        </div>
        <Segmented
          label="Customer location"
          value={inter ? 'inter' : 'intra'}
          onChange={(v) => setInter(v === 'inter')}
          options={[
            { value: 'intra', label: 'Same state' },
            { value: 'inter', label: 'Other state' },
          ]}
        />
      </div>
      <div tabIndex={0} role="region" aria-label="Invoice items" className="mt-[18px] overflow-x-auto">
        <table className="w-full min-w-[440px] border-collapse text-sm tabular-nums">
          <thead>
            <tr className="text-left">
              <th scope="col" className={th}>Item</th>
              <th scope="col" className={th}>Qty</th>
              <th scope="col" className={cn(th, 'text-right')}>Taxable value</th>
            </tr>
          </thead>
          <tbody>
            {[['Cotton Kurta', 799], ['Running Shoes', 2499], ['Paracetamol 500 mg', 30]].map(([n, v]) => (
              <tr key={n} className="border-t border-line">
                <td className="py-2.5 font-semibold text-ink">{n}</td>
                <td>1</td>
                <td className="text-right">{money(v)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <dl className="mt-3 flex flex-col gap-2 rounded-[14px] bg-surface p-4 text-sm tabular-nums text-body" aria-live="polite">
        <div className="flex justify-between"><dt>Taxable value</dt><dd>₹3,328.00</dd></div>
        {inter ? (
          <div className="flex justify-between"><dt>IGST 5%</dt><dd>₹166.40</dd></div>
        ) : (
          <>
            <div className="flex justify-between"><dt>CGST 2.5%</dt><dd>₹83.20</dd></div>
            <div className="flex justify-between"><dt>SGST 2.5%</dt><dd>₹83.20</dd></div>
          </>
        )}
        <div className="flex justify-between border-t border-slate-300 pt-2.5 text-lg font-extrabold text-ink">
          <dt>Total</dt><dd>₹3,494.40</dd>
        </div>
      </dl>
      <Hint>Try it: switch the customer&apos;s state and watch the tax split change.</Hint>
    </div>
  );
}

const ledger = [
  { d: '12 Nov', what: 'Purchase received', ch: '+12', bal: 12 },
  { d: '14 Nov', what: 'Sale INV/000171', ch: '−4', bal: 8 },
  { d: '15 Nov', what: 'Customer return', ch: '+1', bal: 9 },
  { d: '16 Nov', what: 'Adjustment: damaged', ch: '−1', bal: 8 },
  { d: '17 Nov', what: 'Sale INV/000184', ch: '−5', bal: 3 },
];

export function StockMock() {
  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-lg font-extrabold text-ink">Running Shoes · UK 9 · Black</p>
          <p className="text-[13px] text-muted">Main store · stock history</p>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-[30px] font-extrabold text-ink">3</span>
          <span className="rounded-full bg-warn-soft px-2.5 py-1 text-[13px] font-bold text-warn-strong">Low stock</span>
        </div>
      </div>
      <div tabIndex={0} role="region" aria-label="Stock history" className="mt-4 overflow-x-auto">
        <table className="w-full min-w-[480px] border-collapse text-sm tabular-nums">
          <thead>
            <tr className="text-left">
              <th scope="col" className={th}>Date</th>
              <th scope="col" className={th}>What happened</th>
              <th scope="col" className={cn(th, 'text-right')}>Change</th>
              <th scope="col" className={cn(th, 'text-right')}>Balance</th>
            </tr>
          </thead>
          <tbody>
            {ledger.map((l) => (
              <tr key={l.d + l.what} className="border-t border-line">
                <td className="py-[11px] text-muted">{l.d}</td>
                <td className="font-semibold text-ink">{l.what}</td>
                <td className={cn('text-right font-bold', l.ch.startsWith('+') ? 'text-emerald-700' : 'text-red-700')}>{l.ch}</td>
                <td className="text-right text-ink">{l.bal}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Hint>Every sale, return, purchase and adjustment is written down. Nothing is ever overwritten.</Hint>
    </div>
  );
}

const LIMIT = 20000;
const OPENING_DUE = 12450;

export function DuesMock() {
  const [paid, setPaid] = useState(0);
  const due = Math.max(0, OPENING_DUE - paid * 2000);
  const pct = Math.round((due / LIMIT) * 100);
  const statement = [
    { d: '02 Nov', what: 'Sale INV/000132', amt: '+₹8,200.00', debit: true },
    { d: '09 Nov', what: 'Payment by UPI', amt: '−₹3,000.00' },
    { d: '15 Nov', what: 'Sale INV/000168', amt: '+₹7,250.00', debit: true },
  ];
  if (paid > 0) {
    statement.push({ d: '17 Nov', what: `Payment by cash × ${paid}`, amt: `−${money(Math.min(OPENING_DUE, paid * 2000))}` });
  }
  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <span className="flex size-[46px] items-center justify-center rounded-full bg-brand-soft font-extrabold text-brand-strong">MT</span>
          <div>
            <p className="text-lg font-extrabold text-ink">Meena Traders</p>
            <p className="text-[13px] text-muted">Credit limit {money(LIMIT)}</p>
          </div>
        </div>
        <div className="text-right">
          <p className="text-xs font-bold tracking-[0.06em] text-muted uppercase">Due now</p>
          <p className="text-[28px] font-extrabold text-ink tabular-nums" aria-live="polite">{money(due)}</p>
        </div>
      </div>
      <div
        className="mt-4 h-3 overflow-hidden rounded-full bg-line"
        role="progressbar"
        aria-label="Credit limit used"
        aria-valuenow={pct}
        aria-valuemin={0}
        aria-valuemax={100}
      >
        <div className="h-3 rounded-full bg-brand transition-[width] duration-300" style={{ width: `${pct}%` }} />
      </div>
      <p className="mt-1.5 text-[13px] text-muted">{pct}% of credit limit used</p>
      <ul className="mt-4">
        {statement.map((s) => (
          <li key={s.d + s.what} className="flex justify-between gap-3 border-t border-line py-[11px] text-sm tabular-nums">
            <span className="w-16 text-muted">{s.d}</span>
            <span className="flex-1 font-semibold text-ink">{s.what}</span>
            <span className={cn('font-bold', s.debit ? 'text-red-700' : 'text-emerald-700')}>{s.amt}</span>
          </li>
        ))}
      </ul>
      <div className="mt-3.5 flex flex-wrap gap-2.5">
        <Button size="md" onClick={() => setPaid((p) => Math.min(7, p + 1))} disabled={due === 0} className="text-sm font-bold">
          Record ₹2,000 payment
        </Button>
        <Button variant="outline" onClick={() => setPaid(0)} className="text-sm">
          Reset
        </Button>
      </div>
    </div>
  );
}

const RANGES = {
  7: {
    bars: [[38, 'Mon'], [55, 'Tue'], [46, 'Wed'], [72, 'Thu'], [64, 'Fri'], [92, 'Sat'], [80, 'Sun']],
    kpis: [['Sales', '₹1,84,320'], ['Bills', '412'], ['Average bill', '₹447']],
  },
  30: {
    bars: [[62, 'Wk 1'], [74, 'Wk 2'], [58, 'Wk 3'], [90, 'Wk 4']],
    kpis: [['Sales', '₹7,62,900'], ['Bills', '1,706'], ['Average bill', '₹447']],
  },
};

export function ReportsMock() {
  const [range, setRange] = useState(7);
  const { bars, kpis } = RANGES[range];
  const max = Math.max(...bars.map((b) => b[0]));
  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-lg font-extrabold text-ink">
          Sales overview <span className="ml-1.5 text-xs font-semibold text-muted">Sample data</span>
        </p>
        <Segmented
          label="Date range"
          value={range}
          onChange={setRange}
          options={[
            { value: 7, label: 'Last 7 days' },
            { value: 30, label: 'Last 30 days' },
          ]}
        />
      </div>
      <dl className="mt-4 grid grid-cols-[repeat(auto-fit,minmax(140px,1fr))] gap-3">
        {kpis.map(([k, v]) => (
          <div key={k} className="rounded-[14px] border border-line bg-surface p-3.5">
            <dt className="text-[13px] text-muted">{k}</dt>
            <dd className="mt-1 text-[22px] font-extrabold text-ink tabular-nums">{v}</dd>
          </div>
        ))}
      </dl>
      <figure className="mt-4" aria-label={`Sales by ${range === 7 ? 'day' : 'week'}, highest bar is the best ${range === 7 ? 'day' : 'week'}`}>
        <div className="flex h-[170px] items-end gap-2.5 border-b border-slate-300 px-1">
          {bars.map(([h, label]) => (
            <div key={label} className="flex h-full flex-1 items-end justify-center">
              <div
                className={cn('w-full max-w-11 rounded-t-md rounded-b-xs transition-[height] duration-300', h === max ? 'bg-brand' : 'bg-blue-200')}
                style={{ height: `${h}%` }}
              />
            </div>
          ))}
        </div>
        <div className="flex gap-2.5 px-1 pt-1.5">
          {bars.map(([, label]) => (
            <span key={label} className="flex-1 text-center text-xs text-muted">{label}</span>
          ))}
        </div>
      </figure>
      <span className="mt-3.5 inline-flex min-h-11 items-center gap-2 rounded-[10px] border border-emerald-200 bg-white px-4 text-sm font-bold text-ok-strong">
        <Download className="size-4" aria-hidden="true" />
        Export to Excel
      </span>
    </div>
  );
}

const catalogue = [
  { name: 'Cotton Kurta', price: '₹799', icon: Shirt, bg: 'bg-blue-100' },
  { name: 'Denim Jeans', price: '₹1,299', icon: Shirt, bg: 'bg-indigo-100' },
  { name: 'Linen Shirt', price: '₹1,099', icon: Shirt, bg: 'bg-green-100' },
  { name: 'Leather Belt', price: '₹599', icon: Ruler, bg: 'bg-amber-100' },
];

export function CatalogueMock() {
  return (
    <div>
      <div className="overflow-hidden rounded-[14px] border border-line">
        <div className="flex items-center gap-2.5 bg-slate-100 px-3.5 py-2.5">
          <Lock className="size-3.5 text-muted" aria-hidden="true" />
          <span className="flex-1 rounded-lg bg-white px-3 py-1.5 text-[13px] text-slate-700">ravifashion.bilmitra.com</span>
        </div>
        <div className="p-[18px]">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <strong className="text-lg text-ink">Ravi Fashion</strong>
            <span className="text-[13px] text-muted">FC Road, Pune · Open 10 am to 9 pm</span>
          </div>
          <ul className="mt-3.5 grid grid-cols-[repeat(auto-fit,minmax(130px,1fr))] gap-3">
            {catalogue.map((c) => (
              <li key={c.name} className="overflow-hidden rounded-xl border border-line">
                <div className={cn('flex h-[84px] items-center justify-center text-ink', c.bg)}>
                  <c.icon className="size-8" strokeWidth={1.6} aria-hidden="true" />
                </div>
                <div className="p-2.5">
                  <p className="text-sm font-bold text-ink">{c.name}</p>
                  <p className="mt-0.5 text-sm font-bold text-brand-strong">{c.price}</p>
                  <p className="mt-1 text-xs text-ok-strong">In stock</p>
                </div>
              </li>
            ))}
          </ul>
        </div>
      </div>
      <Hint>Customers see your products and prices. Ordering and online payment are not part of it yet.</Hint>
    </div>
  );
}
