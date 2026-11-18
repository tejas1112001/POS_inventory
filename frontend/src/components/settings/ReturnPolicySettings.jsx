import { useEffect, useState } from 'react';
import { SectionCard } from '@/components/shared/section-card';
import { api } from '@/services/api';
import { CONDITIONS, DEFAULT_POLICY } from '@/lib/returns';
import { CheckField, Notice, SaveButton, TextField } from './FormKit';

// ReturnPolicySettings (A18): how this shop takes goods back.
export default function ReturnPolicySettings({ canEdit }) {
  const [policy, setPolicy] = useState(null);
  const [days, setDays] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState(null);
  const [fields, setFields] = useState({});

  useEffect(() => {
    api
      .get('/api/settings/returns', { quiet: true })
      .then((p) => {
        setPolicy({ ...DEFAULT_POLICY, ...p });
        setDays(p.timeLimitDays == null ? '' : String(p.timeLimitDays));
      })
      .catch((err) => setNotice({ kind: 'error', text: err.message }));
  }, []);

  if (!policy) return <SectionCard title="Returns">{notice ? <Notice notice={notice} /> : <p className="text-sm text-muted-foreground">Loading…</p>}</SectionCard>;

  const set = (k) => (v) => setPolicy((p) => ({ ...p, [k]: v }));
  const toggleRestock = (c) => (on) =>
    setPolicy((p) => ({ ...p, restockConditions: on ? [...new Set([...p.restockConditions, c])] : p.restockConditions.filter((x) => x !== c) }));
  const dis = !canEdit || busy;

  const save = async (e) => {
    e.preventDefault();
    setBusy(true);
    setNotice(null);
    setFields({});
    try {
      const out = await api.put('/api/settings/returns', { ...policy, timeLimitDays: days.trim() === '' ? null : Number(days) }, { quiet: true });
      setPolicy({ ...DEFAULT_POLICY, ...out });
      setDays(out.timeLimitDays == null ? '' : String(out.timeLimitDays));
      setNotice({ kind: 'ok', text: 'Return policy saved.' });
    } catch (err) {
      setFields(err.fields || {});
      setNotice({ kind: 'error', text: err.message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <SectionCard title="Returns">
      <form className="space-y-4" onSubmit={save} noValidate>
        <p className="text-sm text-muted-foreground">
          How this shop takes goods back. A return first reduces what the customer still owes on the sale; only the rest is refunded, given as store credit or used for an exchange.
          Users allowed to override the policy can still go ahead, with a written reason.
        </p>
        <div className="grid gap-4 md:grid-cols-2">
          <CheckField id="rp-refund" label="Refunds (cash, UPI, card)" checked={policy.refund} onChange={set('refund')} disabled={dis} />
          <CheckField id="rp-credit" label="Store credit" hint="Named customers only; it never expires." checked={policy.storeCredit} onChange={set('storeCredit')} disabled={dis} />
          <CheckField id="rp-exchange" label="Exchanges" hint="Return and sell new goods in one step." checked={policy.exchange} onChange={set('exchange')} disabled={dis} />
          <CheckField id="rp-linked" label="Only returns of a recorded sale" hint="Returns without a sale are refused." checked={policy.requireLinkedSale} onChange={set('requireLinkedSale')} disabled={dis} />
          <TextField
            id="rp-days"
            type="number"
            min="1"
            max="3650"
            label="Time limit (days after the sale)"
            hint="Empty: no limit."
            value={days}
            onChange={setDays}
            error={fields.timeLimitDays}
            disabled={dis}
          />
          <div className="space-y-2">
            <CheckField
              id="rp-conditions"
              label="Ask the condition of each returned item"
              hint="Only the conditions ticked below go back to stock; others are recorded and stay out of stock."
              checked={policy.conditions}
              onChange={set('conditions')}
              disabled={dis}
            />
            {policy.conditions && (
              <div className="ml-6 space-y-1">
                {CONDITIONS.map((c) => (
                  <CheckField
                    key={c.value}
                    id={`rp-restock-${c.value}`}
                    label={`${c.label} goes back to stock`}
                    checked={policy.restockConditions.includes(c.value)}
                    onChange={toggleRestock(c.value)}
                    disabled={dis}
                  />
                ))}
                {fields.restockConditions && <p className="text-xs text-destructive">{fields.restockConditions}</p>}
              </div>
            )}
          </div>
        </div>
        {fields.refund && <p className="text-sm text-destructive">{fields.refund}</p>}
        <Notice notice={notice} />
        {canEdit && <SaveButton busy={busy}>Save return policy</SaveButton>}
      </form>
    </SectionCard>
  );
}
