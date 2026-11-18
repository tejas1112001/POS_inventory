import { useEffect, useState } from 'react';
import { toast } from '../../../shared/toast/store';
import { SectionCard } from '@/components/shared/section-card';
import { api } from '@/services/api';
import { usePos } from '@/hooks/usePos';
import { loadShopSettings } from '@/hooks/useShopSettings';
import { cn } from '@/lib/utils';
import { Notice, SaveButton } from './FormKit';
import { errorNotice } from './formUtils';

// BusinessSettings: the shop's business type and industry modules
// (industry P1 item 1; GET /api/modules, PUT /api/settings/business).
// Choosing a type ticks its recommended modules; the owner can untick
// them. Only modules:manage (the owner by default) can change it.
export default function BusinessSettings({ canManage }) {
  const { reloadMe } = usePos();
  const [data, setData] = useState(null);
  const [loadError, setLoadError] = useState(null);
  const [type, setType] = useState('retail');
  const [mods, setMods] = useState([]);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState(null);

  const apply = (d) => {
    setData(d);
    setType(d.businessType);
    setMods(d.modules.filter((m) => m.enabled).map((m) => m.key));
  };

  useEffect(() => {
    let live = true;
    api
      .get('/api/modules', { quiet: true })
      .then((d) => live && apply(d))
      .catch((e) => live && setLoadError(e));
    return () => {
      live = false;
    };
  }, []);

  if (!data) {
    return (
      <SectionCard title="Business type">
        {loadError ? <Notice notice={errorNotice(loadError)} /> : <p className="text-sm text-muted-foreground">Loading...</p>}
      </SectionCard>
    );
  }

  const entitled = new Set(data.modules.filter((m) => m.entitled).map((m) => m.key));
  const pickType = (t) => {
    setType(t);
    setMods((data.recommended[t] || []).filter((k) => entitled.has(k)));
  };
  const toggle = (k) => setMods((cur) => (cur.includes(k) ? cur.filter((x) => x !== k) : [...cur, k]));
  const original = data.modules.filter((m) => m.enabled).map((m) => m.key);
  const changed = type !== data.businessType || mods.length !== original.length || mods.some((k) => !original.includes(k));

  const save = async (e) => {
    e.preventDefault();
    if (!changed) {
      toast.info('Nothing to save.');
      return;
    }
    setBusy(true);
    setNotice(null);
    try {
      await api.put('/api/settings/business', { businessType: type, modules: mods }, { quiet: true });
      const fresh = await api.get('/api/modules', { quiet: true });
      apply(fresh);
      await Promise.all([reloadMe(), loadShopSettings(true)]);
      toast.success('Business type saved.');
    } catch (err) {
      setNotice(errorNotice(err));
    } finally {
      setBusy(false);
    }
  };

  const dis = !canManage || busy;
  return (
    <form onSubmit={save} noValidate>
      <SectionCard title="Business type">
        <p className="mb-3 text-sm text-muted-foreground">
          The type suggests modules and starting data; it never blocks anything. Turning a module off hides its screens and
          keeps its data.
        </p>
        <div role="radiogroup" aria-label="Business type" className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
          {data.businessTypes.map((b) => (
            <button
              key={b.key}
              type="button"
              role="radio"
              aria-checked={type === b.key}
              disabled={dis}
              onClick={() => pickType(b.key)}
              className={cn(
                'rounded-lg border-2 p-3 text-left transition-colors disabled:cursor-not-allowed',
                type === b.key ? 'border-primary bg-primary/5' : 'border-border hover:border-primary/40'
              )}
            >
              <span className="block text-sm font-medium">{b.label}</span>
              <span className="mt-1 block text-xs text-muted-foreground">{b.description}</span>
            </button>
          ))}
        </div>

        <h3 className="mb-2 mt-5 text-sm font-medium">Modules</h3>
        <ul className="space-y-2">
          {data.modules.map((m) => {
            const allowed = m.entitled || original.includes(m.key);
            return (
              <li key={m.key} className="flex items-start gap-3 rounded-md border p-3">
                <input
                  id={`module-${m.key}`}
                  type="checkbox"
                  className="mt-1 size-4"
                  checked={mods.includes(m.key)}
                  onChange={() => toggle(m.key)}
                  disabled={dis || !allowed}
                />
                <label htmlFor={`module-${m.key}`} className="min-w-0 flex-1">
                  <span className="block text-sm font-medium">
                    {m.label}
                    {!m.entitled && <span className="ml-2 rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">Not in your plan</span>}
                  </span>
                  <span className="block text-xs text-muted-foreground">{m.description}</span>
                </label>
              </li>
            );
          })}
        </ul>

        <div className="mt-4 flex items-center justify-between gap-3">
          <div className="min-w-0 flex-1">
            <Notice notice={notice} />
          </div>
          {canManage ? (
            <SaveButton busy={busy} disabled={!changed}>Save business type</SaveButton>
          ) : (
            <p className="text-sm text-muted-foreground">Only the owner can change the business type and modules.</p>
          )}
        </div>
      </SectionCard>
    </form>
  );
}
