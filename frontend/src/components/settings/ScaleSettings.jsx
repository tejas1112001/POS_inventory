import { useEffect, useState } from 'react';
import { Plus, Trash2 } from 'lucide-react';
import { SectionCard } from '@/components/shared/section-card';
import { Button } from '@/components/ui/button';
import { api } from '@/services/api';
import { setShopSettings, useShopSettings } from '@/hooks/useShopSettings';
import { toast } from '../../../shared/toast/store';

const NEW_RULE = { prefix: '21', itemDigits: 5, value: 'weight', decimals: 3 };
const sel = 'h-8 rounded-md border bg-background px-2 text-sm';

// example is a sample code of a rule: 21 00042 01250 C.
function example(r) {
  const item = '4'.padStart(r.itemDigits, '0');
  const valueDigits = 10 - r.itemDigits;
  const value = (r.value === 'weight' ? '1250' : '7500').padStart(valueDigits, '0');
  return `${r.prefix} ${item} ${value} + check digit`;
}

// ScaleSettings (scale barcodes, tenant 00044): how this shop's weighing
// scales lay out their labels. Each rule: an in-store prefix (20-29), the
// scale code's digits, and whether the rest is the weight or the price.
// Shop-wide (unlike the scanner settings above).
export default function ScaleSettings({ canEdit }) {
  const { settings } = useShopSettings();
  const [rules, setRules] = useState(settings?.scaleBarcodes || []);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  useEffect(() => setRules(settings?.scaleBarcodes || []), [settings?.scaleBarcodes]);
  const set = (i, patch) => setRules((r) => r.map((x, j) => (j === i ? { ...x, ...patch } : x)));

  const save = async () => {
    setBusy(true);
    setErr('');
    try {
      const saved = await api.put('/api/settings', { scaleBarcodes: rules }, { quiet: true });
      setShopSettings(saved);
      toast.success('Scale labels saved.');
    } catch (e) {
      setErr((e?.fields && Object.values(e.fields)[0]) || e.message || 'Could not save.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <SectionCard title="Weighing scale labels">
      <p className="mb-3 text-sm text-muted-foreground">
        A scale that prints barcodes puts the item&apos;s scale code and its weight or price in an in-store code. Set up how your scales print
        it; give each loose product its scale code under Other barcodes. Products sold by weight need part units turned on.
      </p>
      {rules.length === 0 && <p className="mb-2 text-sm text-muted-foreground">No scale labels: scale codes are not read.</p>}
      <ul className="space-y-2">
        {rules.map((r, i) => (
          <li key={i} className="flex flex-wrap items-center gap-2 text-sm">
            <label className="flex items-center gap-1">
              Starts with
              <select className={sel} value={r.prefix} disabled={!canEdit} onChange={(e) => set(i, { prefix: e.target.value })}>
                {Array.from({ length: 10 }, (_, k) => `2${k}`).map((p) => (
                  <option key={p}>{p}</option>
                ))}
              </select>
            </label>
            <label className="flex items-center gap-1">
              scale code of
              <select className={sel} value={r.itemDigits} disabled={!canEdit} onChange={(e) => set(i, { itemDigits: Number(e.target.value) })}>
                {[4, 5, 6].map((n) => (
                  <option key={n} value={n}>
                    {n} digits
                  </option>
                ))}
              </select>
            </label>
            <label className="flex items-center gap-1">
              then the
              <select
                className={sel}
                value={`${r.value}:${r.decimals}`}
                disabled={!canEdit}
                onChange={(e) => {
                  const [value, decimals] = e.target.value.split(':');
                  set(i, { value, decimals: Number(decimals) });
                }}
              >
                <option value="weight:3">weight in grams (kg products)</option>
                <option value="weight:0">weight in the product&apos;s unit</option>
                <option value="price:2">price in paise</option>
                <option value="price:0">price in rupees</option>
              </select>
            </label>
            <span className="font-mono text-xs text-muted-foreground">{example(r)}</span>
            {canEdit && (
              <Button variant="ghost" size="icon" className="size-8" aria-label={`Remove scale label ${i + 1}`} onClick={() => setRules((x) => x.filter((_, j) => j !== i))}>
                <Trash2 className="size-4" aria-hidden="true" />
              </Button>
            )}
          </li>
        ))}
      </ul>
      {err && (
        <p role="alert" className="mt-2 text-sm text-danger-fg">
          {err}
        </p>
      )}
      {canEdit && (
        <div className="mt-3 flex gap-2">
          <Button
            size="sm"
            variant="outline"
            disabled={rules.length >= 10}
            onClick={() => setRules((x) => [...x, { ...NEW_RULE, prefix: ['21', '22', '23', '24', '25', '26', '27', '28', '29', '20'].find((p) => !x.some((y) => y.prefix === p)) || '21' }])}
          >
            <Plus className="size-4" aria-hidden="true" /> Add a scale label
          </Button>
          <Button size="sm" onClick={save} disabled={busy || JSON.stringify(rules) === JSON.stringify(settings?.scaleBarcodes || [])}>
            {busy ? 'Saving…' : 'Save'}
          </Button>
        </div>
      )}
    </SectionCard>
  );
}
