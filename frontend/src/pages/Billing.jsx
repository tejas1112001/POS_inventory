import { useEffect, useState } from 'react';
import { ArrowRightLeft, CreditCard, ExternalLink, Loader2 } from 'lucide-react';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { SectionCard } from '@/components/shared/section-card';
import { Button } from '@/components/ui/button';
import { usePermission } from '@/context/PermissionContext';
import { api } from '@/services/api';
import { formatDate } from '@/lib/format';
import { cn } from '@/lib/utils';

const STATUS = {
  trialing: ['Trial', 'bg-sky-100 text-sky-800'],
  active: ['Active', 'bg-emerald-100 text-emerald-800'],
  past_due: ['Payment failed', 'bg-red-100 text-red-800'],
  grace: ['Payment overdue', 'bg-red-100 text-red-800'],
  cancelled: ['Cancelled', 'bg-amber-100 text-amber-800'],
  expired: ['Ended', 'bg-muted text-muted-foreground'],
  read_only: ['Read-only', 'bg-red-100 text-red-800'],
  suspended: ['Suspended', 'bg-red-100 text-red-800'],
};

const LIMITS = [
  ['stores', 'max_stores', 'Stores'],
  ['users', 'max_users', 'Users'],
  ['products', 'max_products', 'Products'],
  ['storageMb', 'max_storage_mb', 'File storage (MB)'],
  ['emailsToday', 'max_emails_per_day', 'Emails today'],
];
const FEATURES = {
  reports_export: 'Report export (Excel, PDF)',
  public_shop: 'Public shop page',
  tenant_smtp: 'Your own email server',
  multi_store: 'Several stores',
  custom_domain: 'Custom domain',
};

// Plan prices are in minor units (paise, cents) of the plan's currency.
function money(minor, currency) {
  try {
    return new Intl.NumberFormat('en-IN', { style: 'currency', currency: currency || 'INR' }).format((Number(minor) || 0) / 100);
  } catch {
    return `${(Number(minor) || 0) / 100} ${currency || ''}`;
  }
}

// Only https links from the billing provider are followed.
const safeUrl = (u) => (typeof u === 'string' && /^https:\/\/[^\s"<>]+$/i.test(u) ? u : null);

// Billing shows the shop's plan, usage against its limits and invoices,
// and lets a billing manager start a subscription. Payment happens on the
// provider's page; the shop becomes active only when the provider tells
// the server (never from this page).
export default function Billing() {
  const { hasPermission } = usePermission();
  const canManage = hasPermission('billing', 'manage');
  const [sub, setSub] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState('');
  const [change, setChange] = useState(null); // preview of a plan change

  const load = () =>
    api
      .get('/api/billing/subscription', { quiet: true })
      .then(setSub)
      .catch((err) => setError(err.message));
  useEffect(() => {
    load();
  }, []);

  const failText = (err) => (err.status === 503 ? 'The payment service is not answering. Try again in a few minutes.' : err.message);

  // previewChange asks the server what a change would do before confirming.
  const previewChange = async (planCode) => {
    setBusy(planCode);
    setError('');
    try {
      setChange(await api.get(`/api/billing/plan-change?planCode=${encodeURIComponent(planCode)}`, { quiet: true }));
    } catch (err) {
      setError(failText(err));
    } finally {
      setBusy('');
    }
  };
  const confirmChange = async () => {
    setBusy('confirm');
    try {
      await api.post('/api/billing/plan-change', { planCode: change.to.code }, { quiet: true });
      setChange(null);
      await load();
    } catch (err) {
      setChange(null);
      setError(failText(err));
    } finally {
      setBusy('');
    }
  };
  const cancelScheduled = async () => {
    setBusy('cancel');
    setError('');
    try {
      await api.post('/api/billing/plan-change/cancel', {}, { quiet: true });
      await load();
    } catch (err) {
      setError(failText(err));
    } finally {
      setBusy('');
    }
  };

  const checkout = async (planCode) => {
    setBusy(planCode);
    setError('');
    try {
      const res = await api.post('/api/billing/checkout', { planCode }, { quiet: true });
      const url = safeUrl(res?.url);
      if (!url) throw new Error('The payment page could not be opened.');
      window.location.assign(url);
    } catch (err) {
      setError(
        err.code === 'INVALID_STATE'
          ? 'This shop already has a subscription. Use Change plan below.'
          : err.status === 503
            ? 'The payment service is not answering. Try again in a few minutes.'
            : err.message
      );
      setBusy('');
    }
  };

  if (!sub) {
    return (
      <div className="p-4 text-sm text-muted-foreground">
        {error || (
          <span className="inline-flex items-center gap-2">
            <Loader2 className="size-4 animate-spin" /> Loading billing...
          </span>
        )}
      </div>
    );
  }

  const [label, cls] = STATUS[sub.status] || [sub.status, 'bg-muted'];
  const limits = sub.plan?.limits || {};
  const features = sub.plan?.features || {};
  const pending = safeUrl(sub.checkoutUrl);

  return (
    <div className="flex flex-col gap-3 p-3 sm:p-4">
      {error && (
        <div role="alert" className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {error}
        </div>
      )}

      <SectionCard title="Your plan" action={<span className={cn('rounded-full px-2 py-0.5 text-xs font-medium', cls)}>{label}</span>}>
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <div className="text-2xl font-bold">{sub.plan?.name}</div>
            <div className="text-sm text-muted-foreground">
              {sub.plan?.priceMinor ? `${money(sub.plan.priceMinor, sub.plan.currency)} per ${sub.plan.interval === 'yearly' ? 'year' : 'month'}` : 'Free'}
            </div>
          </div>
          <dl className="grid gap-x-8 gap-y-1 text-sm sm:grid-cols-2">
            {sub.status === 'trialing' && sub.trialEnd && <DateRow label="Trial ends" value={sub.trialEnd} />}
            {sub.currentPeriodEnd && <DateRow label={sub.cancelAtPeriodEnd || sub.status === 'cancelled' ? 'Ends on' : 'Renews on'} value={sub.currentPeriodEnd} />}
            {sub.graceEnd && <DateRow label="Pay by" value={sub.graceEnd} />}
          </dl>
        </div>
        {!sub.billingEnabled && <p className="mt-3 text-sm text-muted-foreground">Online payment is not set up yet. Contact support to change your plan.</p>}
        {sub.pendingChange && (
          <div className="mt-3 flex flex-wrap items-center justify-between gap-2 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900">
            <span>
              Changes to <strong>{sub.pendingChange.plan.name}</strong> on {formatDate(sub.pendingChange.at)}.
            </span>
            {canManage && (
              <Button size="sm" variant="outline" onClick={cancelScheduled} disabled={!!busy}>
                {busy === 'cancel' && <Loader2 className="size-4 animate-spin" />} Cancel the change
              </Button>
            )}
          </div>
        )}
        {/* subscribed is true from the moment a checkout starts, before payment. */}
        {canManage && pending && sub.status !== 'active' && (
          <a href={pending} className="mt-3 inline-flex items-center gap-1.5 text-sm font-medium text-primary hover:underline">
            Continue the payment you started <ExternalLink className="size-3.5" />
          </a>
        )}
      </SectionCard>

      <SectionCard title="Usage">
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {LIMITS.map(([key, limitKey, name]) => (
            <Meter key={key} name={name} used={sub.usage?.[key] ?? 0} limit={limits[limitKey]} />
          ))}
        </div>
        <ul className="mt-4 flex flex-wrap gap-2 text-xs">
          {Object.entries(FEATURES).map(([k, name]) => (
            <li key={k} className={cn('rounded-full border px-2 py-0.5', features[k] ? 'border-emerald-300 text-emerald-800' : 'text-muted-foreground line-through')}>
              {name}
            </li>
          ))}
        </ul>
      </SectionCard>

      {sub.billingEnabled && sub.plans?.length > 0 && !sub.subscribed && (
        <SectionCard title="Choose a plan">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {sub.plans.map((p) => (
              <div key={p.code} className="flex flex-col rounded-lg border p-4">
                <div className="font-semibold">{p.name}</div>
                <div className="mt-1 text-xl font-bold">{money(p.priceMinor, p.currency)}</div>
                <div className="text-xs text-muted-foreground">per {p.interval === 'yearly' ? 'year' : 'month'}</div>
                <ul className="mt-3 flex-1 space-y-0.5 text-xs text-muted-foreground">
                  {LIMITS.filter(([, lk]) => p.limits?.[lk]).map(([, lk, name]) => (
                    <li key={lk}>
                      {name}: {p.limits[lk]}
                    </li>
                  ))}
                </ul>
                {canManage ? (
                  <Button className="mt-3" onClick={() => checkout(p.code)} disabled={!!busy}>
                    {busy === p.code ? <Loader2 className="size-4 animate-spin" /> : <CreditCard className="size-4" />} Subscribe
                  </Button>
                ) : (
                  <p className="mt-3 text-xs text-muted-foreground">Ask the shop owner to subscribe.</p>
                )}
              </div>
            ))}
          </div>
          <p className="mt-3 text-xs text-muted-foreground">You pay on the payment provider&apos;s page. The plan starts as soon as the payment is confirmed.</p>
        </SectionCard>
      )}

      {sub.billingEnabled && sub.subscribed && sub.status === 'active' && canManage && sub.plans?.length > 1 && (
        <SectionCard title="Change plan">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {sub.plans
              .filter((p) => p.code !== sub.plan?.code)
              .map((p) => (
                <div key={p.code} className="flex flex-col rounded-lg border p-4">
                  <div className="font-semibold">{p.name}</div>
                  <div className="mt-1 text-xl font-bold">{money(p.priceMinor, p.currency)}</div>
                  <div className="text-xs text-muted-foreground">per {p.interval === 'yearly' ? 'year' : 'month'}</div>
                  <ul className="mt-3 flex-1 space-y-0.5 text-xs text-muted-foreground">
                    {LIMITS.filter(([, lk]) => p.limits?.[lk]).map(([, lk, name]) => (
                      <li key={lk}>
                        {name}: {p.limits[lk]}
                      </li>
                    ))}
                  </ul>
                  <Button className="mt-3" variant="outline" onClick={() => previewChange(p.code)} disabled={!!busy}>
                    {busy === p.code ? <Loader2 className="size-4 animate-spin" /> : <ArrowRightLeft className="size-4" />} Change to {p.name}
                  </Button>
                </div>
              ))}
          </div>
          <p className="mt-3 text-xs text-muted-foreground">A dearer plan starts now and the difference for this period is charged. A cheaper plan starts when the paid period ends.</p>
        </SectionCard>
      )}

      <Dialog open={!!change} onOpenChange={(open) => !open && setChange(null)}>
        {change && (
          <DialogContent>
            <DialogHeader>
              <DialogTitle>
                {change.kind === 'upgrade' ? 'Upgrade' : 'Change'} to {change.to.name}
              </DialogTitle>
              <DialogDescription>{change.effect}</DialogDescription>
            </DialogHeader>
            {change.blockers?.length > 0 && (
              <div role="alert" className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                <p className="font-medium">Reduce these first:</p>
                <ul className="mt-1 list-disc pl-5">
                  {change.blockers.map((b) => (
                    <li key={b.resource}>{b.resource === 'usage' ? b.label : `${b.label}: ${b.used} in use, the plan allows ${b.limit}`}</li>
                  ))}
                </ul>
              </div>
            )}
            {!change.allowed && !change.blockers?.length && <p className="text-sm text-destructive">{change.refusal}</p>}
            <DialogFooter>
              <Button variant="outline" onClick={() => setChange(null)}>
                Close
              </Button>
              {change.allowed && (
                <Button onClick={confirmChange} disabled={busy === 'confirm'}>
                  {busy === 'confirm' && <Loader2 className="size-4 animate-spin" />}
                  {change.when === 'now' ? 'Change now' : `Schedule for ${formatDate(change.effectiveAt)}`}
                </Button>
              )}
            </DialogFooter>
          </DialogContent>
        )}
      </Dialog>

      <SectionCard title="Invoices">
        {sub.invoices?.length ? (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="py-2 font-medium">Date</th>
                <th className="py-2 font-medium">Amount</th>
                <th className="py-2 font-medium">Status</th>
                <th />
              </tr>
            </thead>
            <tbody className="divide-y">
              {sub.invoices.map((inv) => {
                const url = safeUrl(inv.url);
                return (
                  <tr key={inv.id}>
                    <td className="py-2">{formatDate(inv.paidAt || inv.issuedAt, '-')}</td>
                    <td className="py-2">{money(inv.amountMinor, inv.currency)}</td>
                    <td className="py-2 capitalize">{inv.status.replace('_', ' ')}</td>
                    <td className="py-2 text-right">
                      {url && (
                        <a href={url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-primary hover:underline">
                          {inv.status === 'paid' ? 'View' : 'Pay'} <ExternalLink className="size-3.5" />
                        </a>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        ) : (
          <p className="text-sm text-muted-foreground">No invoices yet.</p>
        )}
      </SectionCard>
    </div>
  );
}

function DateRow({ label, value }) {
  return (
    <div className="flex justify-between gap-4">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium">{formatDate(value)}</dd>
    </div>
  );
}

function Meter({ name, used, limit }) {
  const unlimited = !limit;
  const pct = unlimited ? 0 : Math.min(100, Math.round((used / limit) * 100));
  return (
    <div>
      <div className="flex justify-between text-sm">
        <span>{name}</span>
        <span className="text-muted-foreground">
          {used}
          {unlimited ? '' : ` of ${limit}`}
        </span>
      </div>
      <div className="mt-1 h-2 overflow-hidden rounded-full bg-muted">
        <div className={cn('h-full rounded-full', pct >= 100 ? 'bg-red-500' : pct >= 80 ? 'bg-amber-500' : 'bg-primary')} style={{ width: `${unlimited ? 0 : pct}%` }} />
      </div>
    </div>
  );
}
