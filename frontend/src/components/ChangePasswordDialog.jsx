import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Notice, TextField } from '@/components/settings/FormKit';
import { usePos } from '@/hooks/usePos';
import { api } from '@/services/api';

// ChangePasswordDialog changes the signed-in user's password
// (POST /api/auth/password/change). Every other session of the user ends;
// this one continues with the new session the server returns.
export default function ChangePasswordDialog({ open, onClose }) {
  const { login } = usePos();
  const [form, setForm] = useState({ current: '', next: '', repeat: '' });
  const [fields, setFields] = useState({});
  const [notice, setNotice] = useState(null);
  const [busy, setBusy] = useState(false);
  const set = (k) => (v) => setForm((f) => ({ ...f, [k]: v }));

  const close = () => {
    setForm({ current: '', next: '', repeat: '' });
    setFields({});
    setNotice(null);
    onClose();
  };

  const save = async (e) => {
    e.preventDefault();
    setFields({});
    setNotice(null);
    if (form.next.length < 12) return setFields({ newPassword: 'Use at least 12 characters.' });
    if (form.next !== form.repeat) return setFields({ repeat: 'The two passwords are not the same.' });
    setBusy(true);
    try {
      const body = await api.post('/api/auth/password/change', { currentPassword: form.current, newPassword: form.next }, { quiet: true });
      if (body?.token) await login(body);
      setForm({ current: '', next: '', repeat: '' });
      setNotice({ kind: 'ok', text: 'Password changed. You were signed out on your other devices.' });
    } catch (err) {
      // The server says "is incorrect" next to currentPassword.
      const wrongCurrent = err.code === 'INVALID_CREDENTIALS' || !!err.fields?.currentPassword;
      setFields(wrongCurrent ? { currentPassword: 'The current password is not correct.' } : err.fields || {});
      setNotice({ kind: 'error', text: wrongCurrent ? 'The current password is not correct.' : err.fields ? 'Please correct the marked fields.' : err.message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && close()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Change password</DialogTitle>
        </DialogHeader>
        <form onSubmit={save} className="space-y-4" noValidate>
          <TextField id="pw-current" type="password" label="Current password" value={form.current} onChange={set('current')} error={fields.currentPassword} autoComplete="current-password" />
          <TextField id="pw-new" type="password" label="New password" hint="At least 12 characters." value={form.next} onChange={set('next')} error={fields.newPassword} autoComplete="new-password" />
          <TextField id="pw-repeat" type="password" label="Repeat the new password" value={form.repeat} onChange={set('repeat')} error={fields.repeat} autoComplete="new-password" />
          <Notice notice={notice} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={close}>
              Close
            </Button>
            <Button type="submit" disabled={busy || !form.current || !form.next}>
              {busy && <Loader2 className="size-4 animate-spin" />} Change password
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
