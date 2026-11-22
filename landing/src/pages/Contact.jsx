import { Mail, MapPin, MessageCircle, MonitorPlay, PhoneCall, Rocket } from 'lucide-react';
import { contact, demoChecklist } from '@/content/site';
import { usePageMeta } from '@/lib/usePageMeta';
import { PageHero } from '@/components/layout/PageHero';
import { CheckList } from '@/components/ui/CheckList';
import { Container } from '@/components/ui/Heading';
import { IconTile } from '@/components/ui/IconTile';
import { DemoForm } from '@/sections/contact/DemoForm';
import { FindShop } from '@/sections/contact/FindShop';

const cards = [
  { icon: MessageCircle, tone: 'ok', label: 'Call or WhatsApp', value: contact.phone },
  { icon: Mail, tone: 'brand', label: 'Email', value: contact.email },
  { icon: MapPin, tone: 'warn', label: 'Based in', value: contact.city },
];

const next = [
  { icon: PhoneCall, t: 'We call you', d: `Within ${contact.callbackHours} working hours, to fix a time that suits you.` },
  { icon: MonitorPlay, t: 'Live demo', d: 'We bill a few of your own products and answer your questions.' },
  { icon: Rocket, t: 'Your shop goes live', d: 'We create your shop, import your products and start your 14-day trial.' },
];

export default function Contact() {
  usePageMeta('Contact | BilMitra', 'Book a free BilMitra demo, call us, or open your own shop login.');

  return (
    <>
      <PageHero eyebrow="Contact" title="Talk to a real person" lead="Book a free demo, call us, or open your own shop login." />

      <Container as="section" aria-label="Ways to reach us" className="pt-16 pb-6">
        <ul className="grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-4">
          {cards.map((c) => (
            <li key={c.label} className="flex items-center gap-3.5 rounded-[20px] border border-line bg-white p-6">
              <IconTile icon={c.icon} tone={c.tone} className="size-[46px]" />
              <div className="min-w-0">
                <p className="text-[13px] text-muted">{c.label}</p>
                <p className="text-[17px] font-bold break-words text-ink">{c.value}</p>
              </div>
            </li>
          ))}
        </ul>
      </Container>

      <Container className="flex flex-wrap items-start gap-6 pt-6 pb-14">
        <section
          id="demo"
          tabIndex={-1}
          aria-labelledby="demo-title"
          className="min-w-0 flex-[3_1_520px] scroll-mt-6 rounded-3xl border border-line bg-white p-6 shadow-[0_24px_60px_rgba(15,23,42,.08)] outline-none sm:p-9"
        >
          <h2 id="demo-title" className="text-[28px] font-extrabold tracking-[-0.02em] text-ink">Book a free demo</h2>
          <p className="mt-2 mb-7 text-body">Tell us about your shop. We will show BilMitra with products like yours.</p>
          <DemoForm />
        </section>

        <div className="flex min-w-0 flex-[2_1_340px] flex-col gap-6">
          <FindShop />
          <section aria-labelledby="ready-title" className="rounded-3xl border border-line bg-surface p-7">
            <h2 id="ready-title" className="text-xl font-extrabold text-ink">Keep these ready for the demo</h2>
            <CheckList items={demoChecklist} className="mt-3.5 gap-2.5" />
          </section>
        </div>
      </Container>

      <Container as="section" aria-labelledby="next-title" className="pb-24">
        <h2 id="next-title" className="mb-6 text-[28px] font-extrabold tracking-[-0.03em] text-ink sm:text-[32px]">What happens next</h2>
        <ol className="grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-4">
          {next.map((s, i) => (
            <li key={s.t} className="rounded-[20px] border border-line bg-white p-6">
              <div className="flex items-center gap-3">
                <span className="flex size-9 items-center justify-center rounded-full bg-brand font-extrabold text-white">{i + 1}</span>
                <s.icon className="size-5 text-brand" strokeWidth={1.8} aria-hidden="true" />
              </div>
              <h3 className="mt-3.5 mb-1.5 text-lg font-bold text-ink">{s.t}</h3>
              <p className="text-[15px] leading-relaxed text-body">{s.d}</p>
            </li>
          ))}
        </ol>
      </Container>
    </>
  );
}
