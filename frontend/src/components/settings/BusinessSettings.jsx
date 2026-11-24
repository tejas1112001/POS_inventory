import { useEffect, useState } from 'react';
import { toast } from '../../../shared/toast/store';
import { SectionCard } from '@/components/shared/section-card';
import { api } from '@/services/api';
import { usePos } from '@/hooks/usePos';
import { loadShopSettings } from '@/hooks/useShopSettings';
import { Notice, SaveButton } from './FormKit';
import { errorNotice } from './formUtils';

// BusinessSettings: the shop's business type and industry modules
// (industry P1 item 1; GET /api/modules, PUT /api/settings/business).
// The type is read-only here: support sets it in the platform console
// (which also switches on the type's modules). The owner (modules:manage)
// can switch modules on and off.
export default function BusinessSettings({ canManage }) {
  const { reloadMe } = usePos();
  const [data, setData] = useState(null);
  const [loadError, setLoadError] = useState(null);
  const [mods, setMods] = useState([]);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState(null);

  const apply = (d) => {
    setData(d);
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

  const toggle = (k) => setMods((cur) => (cur.includes(k) ? cur.filter((x) => x !== k) : [...cur, k]));
  const original = data.modules.filter((m) => m.enabled).map((m) => m.key);
  const changed = mods.length !== original.length || mods.some((k) => !original.includes(k));

  const save = async (e) => {
    e.preventDefault();
    if (!changed) {
      toast.info('Nothing to save.');
      return;
    }
    setBusy(true);
    setNotice(null);
    try {
      await api.put('/api/settings/business', { modules: mods }, { quiet: true });
      const fresh = await api.get('/api/modules', { quiet: true });
      apply(fresh);
      await Promise.all([reloadMe(), loadShopSettings(true)]);
      toast.success('Modules saved.');
    } catch (err) {
      setNotice(errorNotice(err));
    } finally {
      setBusy(false);
    }
  };

  const dis = !canManage || busy;
  const typeInfo = data.businessTypes.find((b) => b.key === data.businessType);
  return (
    <form onSubmit={save} noValidate>
      <SectionCard title="Business type">
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl border bg-muted/40 px-4 py-3">
          <div className="min-w-0">
            <p className="text-sm font-semibold">{typeInfo?.label || data.businessType}</p>
            {typeInfo?.description && <p className="text-xs text-muted-foreground">{typeInfo.description}</p>}
          </div>
          <p className="text-xs text-muted-foreground">Set by support. Contact them to change it.</p>
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
            <SaveButton busy={busy} disabled={!changed}>Save modules</SaveButton>
          ) : (
            <p className="text-sm text-muted-foreground">Only the owner can change the modules.</p>
          )}
        </div>
      </SectionCard>
    </form>
  );
}
