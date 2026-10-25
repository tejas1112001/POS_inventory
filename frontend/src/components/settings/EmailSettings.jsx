import { useEffect, useState } from 'react';
import { Loader2, Send, Trash2 } from 'lucide-react';
import { SectionCard } from '@/components/shared/section-card';
import { Button } from '@/components/ui/button';
import { api } from '@/services/api';
import { formatDateTime } from '@/lib/format';
import { CheckField, Field, Notice, SaveButton, SelectField, TextField } from './FormKit';
import { errorNotice, fieldCls } from './formUtils';

const STATUS = {
  unverified: ['Not tested yet', 'bg-amber-100 text-amber-800'],
  ok: ['Working', 'bg-emerald-100 text-emerald-800'],
  failing: ['Failing', 'bg-red-100 text-red-800'],
  disabled: ['Turned off', 'bg-muted text-muted-foreground'],
};

const empty = { host: '', port: 587, username: '', password: '', fromName: '', fromEmail: '', enabled: true, fallbackToPlatform: true, dailyCap: 300 };

function fromServer(s) {
  return {
    host: s.host || '',
    port: s.port || 587,
    username: s.username || '',
    password: '',
    fromName: s.fromName || '',
    fromEmail: s.fromEmail || '',
    enabled: s.configured ? !!s.enabled : true,
    fallbackToPlatform: s.fallbackToPlatform ?? true,
    dailyCap: s.dailyCap || 300,
  };
}

// EmailSettings sets the shop's own SMTP server for receipts and staff
// emails (GET/PUT/DELETE /api/settings/email, POST .../test). The
// password is write-only: the server only says whether one is saved.
export default function EmailSettings({ canEdit }) {
  const [server, setServer] = useState(null);
  const [form, setForm] = useState(empty);
  const [fields, setFields] = useState({});
  const [notice, setNotice] = useState(null);
  const [busy, setBusy] = useState('');
  const [testTo, setTestTo] = useState('');

  const load = async () => {
    try {
      const s = await api.get('/api/settings/email', { quiet: true });
      setServer(s);
      setForm(fromServer(s));
    } catch (err) {
      setNotice(errorNotice(err));
    }
  };
  useEffect(() => {
    load();
  }, []);

  const set = (k) => (v) => setForm((f) => ({ ...f, [k]: v }));
  const dis = !canEdit || !!busy;

  const save = async (e) => {
    e.preventDefault();
    setBusy('save');
    setNotice(null);
    setFields({});
    const body = {
      host: form.host,
      port: Number(form.port),
      username: form.username,
      fromName: form.fromName,
      fromEmail: form.fromEmail,
      enabled: form.enabled,
      fallbackToPlatform: form.fallbackToPlatform,
      dailyCap: Number(form.dailyCap),
    };
    if (form.password) body.password = form.password;
    try {
      await api.put('/api/settings/email', body, { quiet: true });
      await load();
      setNotice({ kind: 'ok', text: 'Saved. Send a test email to check that it works.' });
    } catch (err) {
      setFields(err.fields || {});
      setNotice(errorNotice(err));
    } finally {
      setBusy('');
    }
  };

  const test = async () => {
    setBusy('test');
    setNotice(null);
    try {
      const r = await api.post('/api/settings/email/test', testTo.trim() ? { to: testTo.trim() } : {}, { quiet: true });
      setNotice(
        r.ok
          ? { kind: 'ok', text: r.message || 'Test email sent. Check the inbox.' }
          : { kind: 'error', text: `The test failed: ${r.message || 'no details'}. Check the server, port, username and password.` }
      );
      await load();
    } catch (err) {
      setNotice(err.code === 'RATE_LIMITED' ? { kind: 'error', text: 'Too many tests. Wait a few minutes.' } : errorNotice(err));
    } finally {
      setBusy('');
    }
  };

  const remove = async () => {
    if (!window.confirm('Remove the SMTP settings? Emails then go out through the platform, if allowed.')) return;
    setBusy('delete');
    setNotice(null);
    try {
      await api.delete('/api/settings/email', { quiet: true });
      await load();
      setNotice({ kind: 'ok', text: 'SMTP settings removed.' });
    } catch (err) {
      setNotice(errorNotice(err));
    } finally {
      setBusy('');
    }
  };

  if (!server) {
    return (
      <SectionCard title="Email (SMTP)">
        {notice ? <Notice notice={notice} /> : <p className="text-sm text-muted-foreground">Loading...</p>}
      </SectionCard>
    );
  }

  const [statusText, statusCls] = STATUS[server.configured ? (server.enabled ? server.status : 'disabled') : 'unverified'] || [server.status, 'bg-muted'];

  return (
    <SectionCard
      title="Email (SMTP)"
      action={server.configured && <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${statusCls}`}>{statusText}</span>}
    >
      <p className="mb-4 text-sm text-muted-foreground">
        Receipts and staff emails are sent from your own mail account. Without it, they go out through the platform when that is
        allowed below.
      </p>
      {server.configured && (
        <dl className="mb-4 grid gap-x-6 gap-y-1 text-xs sm:grid-cols-3">
          <div>
            <dt className="text-muted-foreground">Last test</dt>
            <dd>{formatDateTime(server.lastTestAt, 'Never')}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Last email sent</dt>
            <dd>{formatDateTime(server.lastSuccessAt, 'Never')}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Last error</dt>
            <dd>{server.lastErrorCode ? `${server.lastErrorCode}${server.consecutiveFailures ? ` (${server.consecutiveFailures} in a row)` : ''}` : 'None'}</dd>
          </div>
        </dl>
      )}

      <form onSubmit={save} className="space-y-4" noValidate>
        <div className="grid gap-4 md:grid-cols-3">
          <TextField id="smtp-host" label="SMTP server" placeholder="smtp.example.com" value={form.host} onChange={set('host')} error={fields.host} disabled={dis} autoComplete="off" />
          <SelectField id="smtp-port" label="Port" value={String(form.port)} onChange={(v) => set('port')(Number(v))} error={fields.port || fields.tlsMode} disabled={dis}
            options={[{ value: '587', label: '587 (STARTTLS)' }, { value: '465', label: '465 (TLS)' }]} />
          <TextField id="smtp-cap" type="number" min="1" label="Emails per day at most" value={form.dailyCap} onChange={set('dailyCap')} error={fields.dailyCap} disabled={dis} />
          <TextField id="smtp-user" label="Username" value={form.username} onChange={set('username')} error={fields.username} disabled={dis} autoComplete="off" />
          <Field label="Password" htmlFor="smtp-pass" error={fields.password} hint={server.passwordSet ? 'A password is saved. Leave empty to keep it.' : 'Often an app password from your mail provider.'}>
            <input id="smtp-pass" type="password" className={fieldCls} value={form.password} onChange={(e) => set('password')(e.target.value)} disabled={dis} autoComplete="new-password" placeholder={server.passwordSet ? '••••••••' : ''} />
          </Field>
          <div />
          <TextField id="smtp-from-name" label="From name" placeholder="Your shop" value={form.fromName} onChange={set('fromName')} error={fields.fromName} disabled={dis} maxLength={78} />
          <TextField id="smtp-from" type="email" label="From address" hint="Usually the same as the username." value={form.fromEmail} onChange={set('fromEmail')} error={fields.fromEmail} disabled={dis} />
        </div>
        <div className="flex flex-wrap gap-6">
          <CheckField id="smtp-enabled" label="Use this server" checked={form.enabled} onChange={set('enabled')} disabled={dis} />
          <CheckField id="smtp-fallback" label="If it fails, send through the platform" checked={form.fallbackToPlatform} onChange={set('fallbackToPlatform')} disabled={dis} />
        </div>

        <Notice notice={notice} />

        {canEdit && (
          <div className="flex flex-wrap items-center justify-between gap-3 border-t pt-4">
            <div className="flex flex-1 flex-wrap items-center gap-2">
              <input
                type="email"
                className={`${fieldCls} max-w-xs`}
                placeholder="Send the test to (default: you)"
                aria-label="Test email address"
                value={testTo}
                onChange={(e) => setTestTo(e.target.value)}
                disabled={!server.configured || !!busy}
              />
              <Button type="button" variant="outline" onClick={test} disabled={!server.configured || !!busy}>
                {busy === 'test' ? <Loader2 className="size-4 animate-spin" /> : <Send className="size-4" />} Send test
              </Button>
              {server.configured && (
                <Button type="button" variant="ghost" className="text-destructive" onClick={remove} disabled={!!busy}>
                  <Trash2 className="size-4" /> Remove
                </Button>
              )}
            </div>
            <SaveButton busy={busy === 'save'} />
          </div>
        )}
      </form>
    </SectionCard>
  );
}
