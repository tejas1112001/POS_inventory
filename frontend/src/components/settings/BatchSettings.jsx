import { useEffect, useState } from 'react';
import { SectionCard } from '@/components/shared/section-card';
import { api } from '@/services/api';
import { CheckField, Notice, SaveButton, TextField } from './FormKit';

// BatchSettings (industry P3 item 7, A34): near-expiry window, minimum days
// to sell and the owner's daily expiry email.
export default function BatchSettings({ canEdit }) {
  const [s, setS] = useState(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState(null);
  const [fields, setFields] = useState({});

  const show = (v) =>
    setS({
      nearExpiryDays: String(v.nearExpiryDays),
      minDaysToSell: String(v.minDaysToSell),
      expiryDigest: v.expiryDigest,
    });
  useEffect(() => {
    api
      .get('/api/settings/batches', { quiet: true })
      .then(show)
      .catch((err) => setNotice({ kind: 'error', text: err.message }));
  }, []);

  if (!s)
    return (
      <SectionCard title="Batches & expiry">{notice ? <Notice notice={notice} /> : <p className="text-sm text-muted-foreground">Loading…</p>}</SectionCard>
    );

  const set = (k) => (v) => setS((p) => ({ ...p, [k]: v }));
  const dis = !canEdit || busy;
  const save = async (e) => {
    e.preventDefault();
    setBusy(true);
    setNotice(null);
    setFields({});
    try {
      const out = await api.put(
        '/api/settings/batches',
        {
          nearExpiryDays: Number(s.nearExpiryDays),
          minDaysToSell: Number(s.minDaysToSell),
          expiryDigest: s.expiryDigest,
        },
        { quiet: true }
      );
      show(out);
      setNotice({ kind: 'ok', text: 'Batch settings saved.' });
    } catch (err) {
      setFields(err.fields || {});
      setNotice({ kind: 'error', text: err.message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <SectionCard title="Batches & expiry">
      <form className="space-y-4" onSubmit={save} noValidate>
        <div className="grid gap-4 md:grid-cols-2">
          <TextField
            id="bs-near"
            type="number"
            min="1"
            max="730"
            label="Near expiry (days)"
            hint="Receiving warns, and the expiry report and dashboard count stock expiring within this many days."
            value={s.nearExpiryDays}
            onChange={set('nearExpiryDays')}
            error={fields.nearExpiryDays}
            disabled={dis}
          />
          <TextField
            id="bs-min"
            type="number"
            min="0"
            max="365"
            label="Minimum days left to sell"
            hint="A batch closer to expiry than this is not sold. 0: sell until the expiry date."
            value={s.minDaysToSell}
            onChange={set('minDaysToSell')}
            error={fields.minDaysToSell}
            disabled={dis}
          />
          <CheckField
            id="bs-digest"
            label="Daily expiry email to the owner"
            hint="Sent from 08:00 shop time, only on days with expired, expiring, quarantined or recalled stock."
            checked={s.expiryDigest}
            onChange={set('expiryDigest')}
            disabled={dis}
          />
        </div>
        <Notice notice={notice} />
        {canEdit && <SaveButton busy={busy} />}
      </form>
    </SectionCard>
  );
}
