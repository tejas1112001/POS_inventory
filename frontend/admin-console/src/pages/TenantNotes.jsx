import { toast } from '../../../shared/toast/store';
import { useCallback, useEffect, useState } from 'react';
import { api } from '../api';
import { useSession } from '../session';
import { Button, Notice, TextArea } from '../components/ui';
import { formatDateTime } from '../lib/format';

// TenantNotes: a timeline of notes about the shop, newest first. Notes
// cannot be edited or deleted (the database refuses it), so the history
// stays trustworthy; write a new note to correct an old one.
export default function TenantNotes({ tenant }) {
  const { can } = useSession();
  const base = `/tenants/${encodeURIComponent(tenant.id)}/notes`;
  const [notes, setNotes] = useState(null);
  const [error, setError] = useState('');
  const [text, setText] = useState('');
  const [fieldError, setFieldError] = useState('');
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    api
      .get(base)
      .then((n) => {
        setNotes(n || []);
        setError('');
      })
      .catch((err) => setError(err.message));
  }, [base]);
  useEffect(load, [load]);

  const add = async (e) => {
    e.preventDefault();
    const body = text.trim();
    if (!body) return setFieldError('Write the note first.');
    setBusy(true);
    setFieldError('');
    try {
      await api.post(base, { body });
      setText('');
      toast.success('Note added.');
      load();
    } catch (err) {
      setFieldError(err.fields?.body || err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="stack">
      {can('tenants:edit') && (
        <form onSubmit={add} className="card stack" noValidate>
          <TextArea label="New note" hint="Up to 2000 characters. Notes cannot be edited later." value={text} onChange={setText} error={fieldError} maxLength={2000} rows={3} />
          <div className="row right">
            <Button variant="primary" type="submit" busy={busy}>
              Add note
            </Button>
          </div>
        </form>
      )}
      <Notice kind="error">{error}</Notice>
      {notes === null ? (
        <p className="muted">Loading…</p>
      ) : notes.length === 0 ? (
        <p className="muted">No notes yet.</p>
      ) : (
        <ol className="timeline" aria-label="Notes, newest first">
          {notes.map((n) => (
            <li key={n.id}>
              <p className="muted small">
                {formatDateTime(n.createdAt)} · {n.authorName || n.authorEmail}
              </p>
              <p className="pre-line">{n.body}</p>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
