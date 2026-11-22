import { useEffect, useId, useRef, useState } from 'react';
import { CircleAlert, CircleCheck, Send } from 'lucide-react';
import { bizTypes } from '@/content/site';
import { cn } from '@/lib/cn';
import { Button } from '@/components/ui/Button';

const empty = { name: '', shop: '', phone: '', email: '', city: '', biz: bizTypes[0] };

// Indian mobile: 10 digits starting 6-9, optional +91 / 0 prefix.
const normalisePhone = (v) => v.replace(/[\s-]/g, '').replace(/^(\+?91|0)(?=\d{10}$)/, '');
const validators = {
  name: (v) => (v.trim() ? '' : 'Enter your name.'),
  phone: (v) => {
    if (!v.trim()) return 'Enter your mobile number.';
    return /^[6-9]\d{9}$/.test(normalisePhone(v)) ? '' : 'Enter a 10-digit mobile number, like 98xxxxxxxx.';
  },
  email: (v) => {
    if (!v.trim()) return 'Enter your email address.';
    return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v.trim()) ? '' : 'Enter an email address, like name@example.com.';
  },
};

// The form posts to the Go handler POST /contact (backend/internal/landing),
// which needs a signed time stamp from GET /contact/token, the same origin
// and an empty "website" field (spam trap). A form sent less than 2 s after
// its token was issued is dropped as a bot, so a late token waits first.
const MIN_TOKEN_AGE_MS = 2100;
// Server field names → this form's fields.
const serverField = { name: 'name', email: 'email', phone: 'phone', business: 'shop', message: 'city' };

async function fetchToken() {
  const res = await fetch('/contact/token', { headers: { Accept: 'application/json' }, cache: 'no-store' });
  if (!res.ok) throw new Error(`token ${res.status}`);
  const { token } = await res.json();
  return { token, at: Date.now() };
}

const inputCls = (bad) =>
  cn(
    'h-12 w-full rounded-[10px] border bg-white px-3.5 text-[15px] text-ink placeholder:text-slate-400',
    bad ? 'border-red-600' : 'border-slate-300',
  );

function Field({ id, label, error, hint, children }) {
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={id} className="text-sm font-semibold text-ink">
        {label}
        {hint && <span className="ml-1 font-normal text-muted">{hint}</span>}
      </label>
      {children}
      {error && (
        <p id={`${id}-err`} className="flex items-center gap-1.5 text-sm text-red-700">
          <CircleAlert className="size-4 shrink-0" aria-hidden="true" />
          {error}
        </p>
      )}
    </div>
  );
}

export function DemoForm() {
  const uid = useId();
  const [v, setV] = useState(empty);
  const [errors, setErrors] = useState({});
  const [sent, setSent] = useState(null);
  const [sending, setSending] = useState(false);
  const [formError, setFormError] = useState('');
  const [token, setToken] = useState(null);
  const summaryRef = useRef(null);
  const trapRef = useRef(null);

  useEffect(() => {
    let live = true;
    fetchToken()
      .then((t) => live && setToken(t))
      .catch(() => {}); // fetched again on submit
    return () => {
      live = false;
    };
  }, []);

  const set = (k) => (e) => {
    setV((s) => ({ ...s, [k]: e.target.value }));
    // Clear an error as soon as the field becomes valid.
    if (errors[k] && validators[k] && !validators[k](e.target.value)) setErrors((er) => ({ ...er, [k]: '' }));
  };
  const blur = (k) => () => validators[k] && setErrors((er) => ({ ...er, [k]: validators[k](v[k]) }));

  const submit = async (e) => {
    e.preventDefault();
    if (sending) return;
    const next = Object.fromEntries(Object.entries(validators).map(([k, fn]) => [k, fn(v[k])]));
    setErrors(next);
    if (Object.values(next).some(Boolean)) {
      requestAnimationFrame(() => summaryRef.current?.focus());
      return;
    }
    setSending(true);
    setFormError('');
    try {
      let t = token;
      if (!t) {
        t = await fetchToken();
        setToken(t);
      }
      const wait = t.at + MIN_TOKEN_AGE_MS - Date.now();
      if (wait > 0) await new Promise((r) => setTimeout(r, wait));

      const city = v.city.trim();
      const body = new URLSearchParams({
        ft: t.token,
        topic: 'demo',
        name: v.name.trim(),
        email: v.email.trim(),
        business: v.shop.trim(),
        phone: normalisePhone(v.phone),
        message: [city && `City: ${city}`, `Sells: ${v.biz}`].filter(Boolean).join('\n'),
        website: trapRef.current?.value ?? '',
      });
      const res = await fetch('/contact', {
        method: 'POST',
        headers: { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' },
        body,
      });
      const data = await res.json().catch(() => ({}));
      if (res.ok && data.ok) {
        setSent({ name: v.name.trim(), biz: v.biz.toLowerCase() });
        return;
      }
      if (res.status === 422 && data.error === 'expired') {
        setToken(await fetchToken().catch(() => null));
        setFormError('This page was open for a long time. Please press "Request my demo" again.');
        return;
      }
      if (res.status === 422 && data.errors) {
        setErrors(Object.fromEntries(Object.entries(data.errors).map(([k, m]) => [serverField[k] ?? 'name', m])));
        requestAnimationFrame(() => summaryRef.current?.focus());
        return;
      }
      if (res.status === 429) {
        setFormError('Too many requests from your connection. Please wait a few minutes and try again.');
        return;
      }
      setFormError('We could not send your request just now. Please try again in a few minutes.');
    } catch {
      setFormError('We could not send your request. Check your internet connection and try again.');
    } finally {
      setSending(false);
    }
  };

  if (sent) {
    return (
      <div role="status" className="rounded-2xl border border-emerald-200 bg-ok-soft p-6 text-ok-strong">
        <p className="flex items-center gap-2 text-lg font-bold">
          <CircleCheck className="size-5" aria-hidden="true" />
          Thank you, {sent.name}.
        </p>
        <p className="mt-2 mb-4 text-[15px]">We got your request for a {sent.biz} shop. We will call you soon.</p>
        <Button
          variant="outline"
          className="border-emerald-300 text-ok-strong"
          onClick={() => {
            setV(empty);
            setSent(null);
          }}
        >
          Send another request
        </Button>
      </div>
    );
  }

  const ids = { name: `${uid}-name`, shop: `${uid}-shop`, phone: `${uid}-phone`, email: `${uid}-email`, city: `${uid}-city` };
  const failed = Object.entries(errors).filter(([, m]) => m);

  return (
    <form noValidate onSubmit={submit} className="relative" aria-busy={sending}>
      {/* Spam trap: people never see or fill it. */}
      <div aria-hidden="true" className="absolute -left-[10000px] top-0 size-px overflow-hidden">
        <label>
          Website
          <input ref={trapRef} type="text" name="website" tabIndex={-1} autoComplete="off" defaultValue="" />
        </label>
      </div>

      {failed.length > 0 && (
        <div ref={summaryRef} tabIndex={-1} role="alert" className="mb-6 rounded-xl border border-red-200 bg-red-50 p-4 outline-none">
          <p className="font-bold text-red-800">Please fix {failed.length === 1 ? 'this' : 'these'}:</p>
          <ul className="mt-1.5 list-disc pl-5 text-sm text-red-800">
            {failed.map(([k, m]) => (
              <li key={k}>
                <a href={`#${ids[k]}`} className="underline underline-offset-2" onClick={(e) => { e.preventDefault(); document.getElementById(ids[k])?.focus(); }}>
                  {m}
                </a>
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="grid grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-[18px]">
        <Field id={ids.name} label="Your name" error={errors.name}>
          <input id={ids.name} type="text" autoComplete="name" maxLength={120} value={v.name} onChange={set('name')} onBlur={blur('name')}
            placeholder="Ravi Kumar" aria-invalid={!!errors.name} aria-describedby={errors.name ? `${ids.name}-err` : undefined}
            className={inputCls(errors.name)} />
        </Field>
        <Field id={ids.shop} label="Shop name" hint="(optional)">
          <input id={ids.shop} type="text" autoComplete="organization" maxLength={120} value={v.shop} onChange={set('shop')}
            placeholder="Ravi Fashion" className={inputCls(false)} />
        </Field>
        <Field id={ids.phone} label="Mobile number" error={errors.phone}>
          <input id={ids.phone} type="tel" inputMode="tel" autoComplete="tel-national" value={v.phone} onChange={set('phone')} onBlur={blur('phone')}
            placeholder="98xxxxxxxx" aria-invalid={!!errors.phone} aria-describedby={errors.phone ? `${ids.phone}-err` : undefined}
            className={inputCls(errors.phone)} />
        </Field>
        <Field id={ids.email} label="Email" error={errors.email}>
          <input id={ids.email} type="email" inputMode="email" autoComplete="email" maxLength={254} value={v.email} onChange={set('email')} onBlur={blur('email')}
            placeholder="ravi@example.com" aria-invalid={!!errors.email} aria-describedby={errors.email ? `${ids.email}-err` : undefined}
            className={inputCls(errors.email)} />
        </Field>
        <Field id={ids.city} label="City" hint="(optional)">
          <input id={ids.city} type="text" autoComplete="address-level2" maxLength={120} value={v.city} onChange={set('city')}
            placeholder="Pune" className={inputCls(false)} />
        </Field>
      </div>

      <fieldset className="mt-[22px]">
        <legend className="mb-2.5 text-sm font-semibold text-ink">What do you sell?</legend>
        <div className="flex flex-wrap gap-2.5">
          {bizTypes.map((b) => (
            <label
              key={b}
              className={cn(
                'inline-flex min-h-11 cursor-pointer items-center rounded-full border px-[18px] text-[15px] font-semibold transition-colors',
                'has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-blue-500',
                v.biz === b ? 'border-brand bg-brand text-white' : 'border-slate-300 bg-white text-ink hover:border-slate-400',
              )}
            >
              <input type="radio" name={`${uid}-biz`} value={b} checked={v.biz === b} onChange={set('biz')} className="sr-only" />
              {b}
            </label>
          ))}
        </div>
      </fieldset>

      {formError && (
        <p role="alert" className="mt-[22px] flex items-start gap-2 rounded-xl border border-red-200 bg-red-50 p-4 text-sm font-semibold text-red-800">
          <CircleAlert className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          {formError}
        </p>
      )}

      <Button type="submit" size="lg" disabled={sending} className="mt-[26px] w-full font-bold">
        <Send className="size-[18px]" aria-hidden="true" />
        {sending ? 'Sending…' : 'Request my demo'}
      </Button>
    </form>
  );
}
