import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Eye, EyeOff, Loader2 } from 'lucide-react';
import AuthLayout, { FormMessage } from '@/components/AuthLayout';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { api } from '@/services/api';

const MIN_LENGTH = 12;

// readToken takes the one-time token from the link's fragment
// (/reset-password#token=…). The fragment never reaches the server or a
// Referer header; it is removed from the address bar right away.
// The first read is kept: React may run the initializer twice in
// development, and the fragment is gone after the first.
let captured = null;

function readToken() {
  if (captured === null) {
    captured = new URLSearchParams(window.location.hash.slice(1)).get('token') || '';
    if (window.location.hash) {
      window.history.replaceState(null, '', window.location.pathname);
    }
  }
  return captured;
}

const TEXT = {
  reset: {
    title: 'Choose a new password',
    subtitle: 'After this, every device signed in to your account is signed out.',
    endpoint: '/api/auth/password/reset',
    done: 'Your password has been changed. Sign in with the new password.',
    bad: 'This reset link is not valid any more. Ask for a new one.',
  },
  invite: {
    title: 'Set your password',
    subtitle: 'Choose a password to finish setting up your account.',
    endpoint: '/api/auth/invite/accept',
    done: 'Your account is ready. Sign in with your email and the new password.',
    bad: 'This invitation link is not valid any more. Ask the shop owner to send a new one.',
  },
};

export default function SetPassword({ purpose }) {
  const t = TEXT[purpose];
  const [token] = useState(readToken);
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState('');

  const submit = async (e) => {
    e.preventDefault();
    setError('');
    if (password.length < MIN_LENGTH) {
      setError(`Use at least ${MIN_LENGTH} characters.`);
      return;
    }
    if (password !== confirm) {
      setError('The two passwords are not the same.');
      return;
    }
    setBusy(true);
    try {
      await api.post(t.endpoint, { token, password }, { quiet: true });
      setDone(true);
    } catch (err) {
      if (err.code === 'VALIDATION_FAILED' && err.fields?.password) setError(err.fields.password);
      else if (err.code === 'VALIDATION_FAILED' || err.status === 401 || err.status === 404) setError(t.bad);
      else setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  if (!token) {
    return (
      <AuthLayout title={t.title}>
        <FormMessage>{t.bad}</FormMessage>
        <BackLink purpose={purpose} />
      </AuthLayout>
    );
  }

  return (
    <AuthLayout title={t.title} subtitle={t.subtitle}>
      <FormMessage>{error}</FormMessage>
      {done ? (
        <>
          <FormMessage kind="ok">{t.done}</FormMessage>
          <Button asChild className="mt-5 h-11 w-full">
            <Link to="/login">Sign in</Link>
          </Button>
        </>
      ) : (
        <form onSubmit={submit} className="mt-5 space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="password">New password</Label>
            <div className="relative">
              <Input
                id="password"
                type={show ? 'text' : 'password'}
                className="pr-8"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="new-password"
                minLength={MIN_LENGTH}
                required
              />
              <button
                type="button"
                onClick={() => setShow((v) => !v)}
                className="absolute right-2 top-1/2 -translate-y-1/2 rounded-sm p-0.5 text-muted-foreground hover:text-foreground"
                aria-label={show ? 'Hide password' : 'Show password'}
              >
                {show ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
              </button>
            </div>
            <p className="text-xs text-muted-foreground">At least {MIN_LENGTH} characters. Avoid your name, email or shop name.</p>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="confirm">Repeat the password</Label>
            <Input
              id="confirm"
              type={show ? 'text' : 'password'}
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              autoComplete="new-password"
              required
            />
          </div>
          <Button type="submit" className="h-11 w-full" disabled={busy}>
            {busy && <Loader2 className="size-4 animate-spin" />}
            Save password
          </Button>
        </form>
      )}
      {!done && <BackLink purpose={purpose} />}
    </AuthLayout>
  );
}

function BackLink({ purpose }) {
  return (
    <p className="mt-5 text-center text-sm">
      {purpose === 'reset' ? (
        <Link to="/forgot-password" className="font-medium text-primary hover:underline">
          Ask for a new link
        </Link>
      ) : (
        <Link to="/login" className="font-medium text-primary hover:underline">
          Back to sign in
        </Link>
      )}
    </p>
  );
}
