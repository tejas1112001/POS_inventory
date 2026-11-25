import { useState } from 'react';
import { ArrowRight, Check, FileText, Mail, Minus, Plus, Printer, ReceiptText, TriangleAlert, Wallet } from 'lucide-react';
import { heroBill, heroPoints } from '@/content/site';
import { money } from '@/lib/format';
import { Button } from '@/components/ui/Button';
import { CheckList } from '@/components/ui/CheckList';
import { Container } from '@/components/ui/Heading';
import { HeroBackdrop } from './HeroBackdrop';

const SHOES_IN_STOCK = 5;

function BillMockup() {
  const [qty, setQty] = useState([1, 1, 2]);
  const change = (i, d) =>
    setQty((q) => q.map((v, j) => (j === i ? Math.max(0, Math.min(heroBill[i].max, v + d)) : v)));

  const subtotal = heroBill.reduce((s, p, i) => s + p.price * qty[i], 0);
  const count = qty.reduce((a, b) => a + b, 0);
  const half = Math.round(subtotal * 0.025 * 100) / 100;
  const total = subtotal + half * 2;

  const action = 'flex min-h-11 short:min-h-10 flex-[1_1_80px] items-center justify-center gap-1.5 rounded-[10px] border border-slate-300 bg-white text-sm font-semibold text-ink';

  return (
    // data-tilt: the whole mockup tilts toward the cursor; the badges float above it.
    <div data-tilt="6" className="relative w-full max-w-[580px] min-w-0 pt-7 pb-8 transition-transform short:pt-6 short:pb-6 lg:order-first duration-300 ease-out transform-3d">
      <div aria-hidden="true" className="absolute inset-x-[30px] top-10 bottom-5 rounded-[40px] bg-brand opacity-30 blur-[90px]" />

      <div
        role="group"
        aria-label="Sample bill. Try changing the quantities."
        className="relative overflow-hidden rounded-[20px] border border-line bg-white text-ink shadow-[0_30px_80px_rgba(6,30,36,.55)]"
      >
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-line bg-surface px-[22px] py-[18px] short:py-3">
          <div>
            <p className="text-xs font-semibold tracking-[0.08em] text-body uppercase">New bill · Counter 1</p>
            <p className="mt-0.5 font-bold">INV/2026-27/000184</p>
          </div>
          <span className="flex items-center gap-2 rounded-full bg-ok-soft px-3 py-1.5 text-[13px] font-semibold text-ok-strong">
            <Check className="size-4" strokeWidth={2.4} aria-hidden="true" />
            Prices checked by server
          </span>
        </div>

        <ul className="px-[22px] py-2 short:py-1">
          {heroBill.map((p, i) => (
            <li key={p.name} className="flex items-center gap-3.5 border-b border-dashed border-line py-3.5 short:py-2.5">
              <span className="flex size-[42px] shrink-0 items-center justify-center rounded-[10px] bg-brand-soft text-brand max-[379px]:hidden short:size-9">
                <p.icon className="size-5" strokeWidth={1.8} aria-hidden="true" />
              </span>
              <div className="min-w-0 flex-auto">
                <p className="text-[15px] font-semibold">{p.name}</p>
                <p className="truncate text-[13px] text-muted">{p.meta} · GST 5%</p>
              </div>
              <div className="flex shrink-0 items-center overflow-hidden rounded-[10px] border border-slate-300">
                <button
                  type="button"
                  onClick={() => change(i, -1)}
                  disabled={qty[i] === 0}
                  aria-label={`Remove one ${p.name}`}
                  className="flex size-11 shrink-0 items-center justify-center hover:bg-slate-50 disabled:text-slate-300 sm:size-9"
                >
                  <Minus className="size-4" aria-hidden="true" />
                </button>
                <span className="w-[30px] text-center text-[15px] font-bold tabular-nums" aria-live="polite">
                  {qty[i]}
                </span>
                <button
                  type="button"
                  onClick={() => change(i, 1)}
                  disabled={qty[i] === p.max}
                  aria-label={`Add one ${p.name}`}
                  className="flex size-11 shrink-0 items-center justify-center hover:bg-slate-50 disabled:text-slate-300 sm:size-9"
                >
                  <Plus className="size-4" aria-hidden="true" />
                </button>
              </div>
              <p className="hidden w-[104px] text-right text-[15px] font-bold tabular-nums sm:block">{money(p.price * qty[i])}</p>
            </li>
          ))}
        </ul>

        <dl className="flex flex-col gap-2 px-[22px] pt-3.5 pb-1 short:gap-1 short:pt-2.5 text-sm tabular-nums text-body">
          <div className="flex justify-between"><dt>Subtotal ({count} items)</dt><dd>{money(subtotal)}</dd></div>
          <div className="flex justify-between"><dt>CGST 2.5%</dt><dd>{money(half)}</dd></div>
          <div className="flex justify-between"><dt>SGST 2.5%</dt><dd>{money(half)}</dd></div>
        </dl>
        <div className="mx-[22px] mt-3 flex items-baseline justify-between border-t-2 border-ink py-3.5 short:mt-2 short:py-2.5">
          <span className="text-[15px] font-bold">Total</span>
          <span className="text-2xl font-extrabold sm:text-[28px] short:text-2xl tracking-[-0.02em] tabular-nums">{money(total)}</span>
        </div>

        {/* Illustration of the bill actions; not interactive on this page. */}
        <div aria-hidden="true" className="flex flex-wrap gap-2.5 px-[22px] pt-1.5 pb-[22px] short:pb-4">
          <span className={action}><Printer className="size-4" />Print</span>
          <span className={action}><FileText className="size-4" />PDF</span>
          <span className={action}><Mail className="size-4" />Email</span>
          <span className="flex min-h-11 short:min-h-10 flex-[2_1_160px] items-center justify-center gap-2 rounded-[10px] bg-brand text-[15px] font-bold text-white">
            <Wallet className="size-[18px]" />
            Charge {money(total)}
          </span>
        </div>
      </div>

      <div className="absolute top-0 right-[18px] flex translate-z-10 items-center gap-2.5 rounded-[14px] border border-blue-400/40 bg-ink px-3.5 py-2.5 text-slate-200 shadow-[0_12px_30px_rgba(6,30,36,.5)]">
        <TriangleAlert className="size-4 text-warn" aria-hidden="true" />
        <span className="text-[13px]">
          <strong className="text-white">Running Shoes UK 9</strong> · {SHOES_IN_STOCK - qty[1]} left
        </span>
      </div>
      <div className="absolute bottom-0 left-[18px] flex translate-z-10 items-center gap-2.5 rounded-[14px] border border-emerald-400/40 bg-ink px-3.5 py-2.5 text-slate-200 shadow-[0_12px_30px_rgba(6,30,36,.5)]">
        <ReceiptText className="size-[18px] text-ok" aria-hidden="true" />
        <span className="text-[13px]">
          GST invoice ready · <strong className="text-white">CGST + SGST</strong>
        </span>
      </div>
    </div>
  );
}

export function HomeHero() {
  return (
    <section className="relative isolate bg-night text-slate-200">
      <HeroBackdrop />
      {/* One column (text, then the bill) up to lg; two columns from lg, filling the
          screen under the sticky header (about 78 px) with the content centred. */}
      <Container className="grid items-center justify-items-start gap-10 pt-10 pb-16 sm:pt-14 sm:pb-20 lg:min-h-[calc(100svh-4.875rem)] lg:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)] lg:content-center lg:gap-12 lg:py-12 xl:gap-16 short:py-6">
        <div className="min-w-0">
          <p className="inline-flex items-center gap-2 rounded-full border border-blue-400/35 bg-blue-600/12 px-3.5 py-[7px] text-[13px] font-semibold text-blue-200">
            <span className="size-[7px] rounded-full bg-brand-bright" aria-hidden="true" />
            Point of sale for Indian retail
          </p>
          <h1 className="mt-[22px] text-[34px] short:mt-4 leading-[1.06] font-extrabold tracking-[-0.035em] text-white min-[380px]:text-[40px] sm:text-5xl xl:text-[58px] 2xl:text-[64px] short:xl:text-[52px]">
            GST billing and stock
            <br />
            <span className="text-brand-bright">for Indian shops.</span>
          </h1>
          <p className="mt-[22px] max-w-[560px] text-base short:mt-4 leading-relaxed text-slate-300 sm:text-lg">
            MintoBill is point of sale software for Indian shops. Make GST bills, keep your stock up to date and
            follow your sales, all from a web browser.
          </p>
          <div className="btn-row mt-8 short:mt-6">
            <Button to="/contact#demo" size="lg" className="font-bold">
              Book a free demo
              <ArrowRight className="size-[18px]" aria-hidden="true" />
            </Button>
            <Button to="/features" variant="ghost" size="lg">
              See all features
            </Button>
          </div>
          <CheckList items={heroPoints} tone="dark" className="mt-[34px] short:mt-6 short:gap-2" />
        </div>

        <BillMockup />
      </Container>
    </section>
  );
}
