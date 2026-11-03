import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { CheckCircle2, Circle, Rocket } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { useConfirm } from '@/components/feedback/ConfirmDialog';
import { usePermission } from '@/context/PermissionContext';
import { api } from '@/services/api';

// OnboardingCard (S10 item 8): a new shop's first steps on the dashboard,
// ticked from the shop's own data, each linking to its page. Shown to
// users who can edit settings until everything is done or it is hidden
// for good.
export default function OnboardingCard() {
  const { hasPermission } = usePermission();
  const confirm = useConfirm();
  const can = hasPermission('settings', 'edit');
  const [data, setData] = useState(null);

  useEffect(() => {
    if (!can) return;
    let live = true;
    api
      .get('/api/onboarding', { quiet: true })
      .then((d) => live && setData(d))
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [can]);

  if (!can || !data || data.hidden || data.done >= data.total) return null;

  const hide = async () => {
    const ok = await confirm({
      title: 'Hide the checklist?',
      body: 'It will not come back, for anyone in this shop. Every step stays available from the menu.',
      confirmLabel: 'Hide checklist',
    });
    if (!ok) return;
    try {
      await api.post('/api/onboarding/hide', {}, { quiet: true });
      setData((d) => ({ ...d, hidden: true }));
    } catch {
      /* stays visible; nothing lost */
    }
  };

  const pct = Math.round((data.done / data.total) * 100);
  return (
    <section className="rounded-lg border bg-card p-4 shadow-xs" aria-labelledby="onb-h">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h2 id="onb-h" className="flex items-center gap-2 text-base font-semibold">
            <Rocket className="size-4 text-primary" aria-hidden="true" /> Get your shop ready
          </h2>
          <p className="text-sm text-muted-foreground">
            {data.done} of {data.total} done
          </p>
        </div>
        <Button type="button" variant="ghost" size="sm" onClick={hide}>
          Hide checklist
        </Button>
      </div>
      <div className="mt-2 h-2 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label="Checklist progress">
        <div className="h-full rounded-full bg-primary" style={{ width: `${pct}%` }} />
      </div>
      <ul className="mt-3 grid gap-2 sm:grid-cols-2">
        {data.steps.map((s) => (
          <li key={s.key}>
            <Link to={s.path} className="flex items-start gap-2 rounded-md p-2 hover:bg-muted">
              {s.done ? (
                <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-success" aria-label="Done" />
              ) : (
                <Circle className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-label="Not done" />
              )}
              <span>
                <span className={s.done ? 'text-sm font-medium text-muted-foreground line-through' : 'text-sm font-medium'}>{s.label}</span>
                {!s.done && <span className="block text-xs text-muted-foreground">{s.hint}</span>}
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}
