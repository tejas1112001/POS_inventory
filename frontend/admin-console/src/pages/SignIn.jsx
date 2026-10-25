import { useState } from 'react';
import { api } from '../api';
import { useSession } from '../session';
import { Button, Check, Notice, TextInput } from '../components/ui';
import Qr from '../components/Qr';
import styles from './SignIn.module.css';

const MIN_PASSWORD = 12;

function message(err) {
  switch (err?.code) {
    case 'INVALID_CREDENTIALS':
      return 'The email or password is not correct.';
    case 'ACCOUNT_LOCKED':
    case 'RATE_LIMITED':
      return 'Too many attempts. Wait a few minutes and try again.';
    case 'UNAUTHENTICATED':
      return 'This sign-in step expired. Start again.';
    default:
      return err?.fields ? Object.values(err.fields).join(' ') : err?.message || 'Sign-in failed.';
  }
}

// SignIn walks the platform sign-in (TENANCY §7): password; a forced
// password change on first sign-in; authenticator enrolment (with the
// recovery codes, shown once); or the authenticator code (or a recovery
// code) on every later sign-in.
export default function SignIn() {
  const { signedIn } = useSession();
  const [step, setStep] = useState('password');
  const [challenge, setChallenge] = useState('');
  const [form, setForm] = useState({ email: '', password: '', next: '', repeat: '', code: '', recovery: '' });
  const [useRecovery, setUseRecovery] = useState(false);
  const [enrol, setEnrol] = useState(null); // {secret, otpauthUri}
  const [codes, setCodes] = useState(null); // {result, list}
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const set = (k) => (v) => setForm((f) => ({ ...f, [k]: v }));

  const restart = () => {
    setStep('password');
    setChallenge('');
    setForm((f) => ({ ...f, password: '', next: '', repeat: '', code: '', recovery: '' }));
  };

  // next handles a login-step answer: another step, or a session.
  const next = async (res) => {
    if (res.accessToken) {
      if (res.recoveryCodes?.length) {
        setCodes({ result: res, list: res.recoveryCodes });
        setStep('codes');
      } else {
        signedIn(res);
      }
      return;
    }
    setChallenge(res.challengeId);
    if (res.passwordChangeRequired) setStep('change');
    else if (res.mfaEnrollRequired) {
      setEnrol(await api.post('/auth/mfa/enroll', { challengeId: res.challengeId }));
      setStep('enrol');
    } else if (res.mfaRequired) setStep('mfa');
  };

  const run = (fn) => async (e) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await fn();
    } catch (err) {
      setError(message(err));
      if (err.code === 'UNAUTHENTICATED') restart();
    } finally {
      setBusy(false);
    }
  };

  const login = run(async () => next(await api.post('/auth/login', { email: form.email.trim(), password: form.password })));
  const change = run(async () => {
    if (form.next.length < MIN_PASSWORD) throw new Error(`Use at least ${MIN_PASSWORD} characters.`);
    if (form.next !== form.repeat) throw new Error('The two passwords are not the same.');
    await next(await api.post('/auth/password/change', { challengeId: challenge, newPassword: form.next }));
  });
  const confirmEnrol = run(async () => next(await api.post('/auth/mfa/enroll/confirm', { challengeId: challenge, code: form.code.trim() })));
  const verify = run(async () =>
    next(await api.post('/auth/mfa/verify', useRecovery ? { challengeId: challenge, recoveryCode: form.recovery.trim() } : { challengeId: challenge, code: form.code.trim() }))
  );

  return (
    <main className={styles.page}>
      <div className={`card ${styles.card}`}>
        <h1>Platform console</h1>
        <p className="muted">For platform staff only.</p>

        {step === 'password' && (
          <form onSubmit={login} className="stack">
            <TextInput label="Email" type="email" value={form.email} onChange={set('email')} autoComplete="username" required />
            <TextInput label="Password" type="password" value={form.password} onChange={set('password')} autoComplete="current-password" required />
            <Notice kind="error">{error}</Notice>
            <Button type="submit" variant="primary" busy={busy}>
              Continue
            </Button>
          </form>
        )}

        {step === 'change' && (
          <form onSubmit={change} className="stack">
            <Notice kind="info">Choose your own password before you continue.</Notice>
            <TextInput label="New password" type="password" value={form.next} onChange={set('next')} autoComplete="new-password" hint={`At least ${MIN_PASSWORD} characters.`} />
            <TextInput label="Repeat the new password" type="password" value={form.repeat} onChange={set('repeat')} autoComplete="new-password" />
            <Notice kind="error">{error}</Notice>
            <Button type="submit" variant="primary" busy={busy}>
              Save password
            </Button>
          </form>
        )}

        {step === 'enrol' && enrol && (
          <form onSubmit={confirmEnrol} className="stack">
            <Notice kind="info">Every sign-in needs a code from an authenticator app. Scan this code with the app, then enter the 6-digit code it shows.</Notice>
            <div className={styles.qr}>
              <Qr text={enrol.otpauthUri} label="Authenticator enrolment code" />
            </div>
            <p className="small muted">
              Can&apos;t scan? Enter this key in the app: <span className="mono">{enrol.secret}</span>
            </p>
            <TextInput label="Code from the app" value={form.code} onChange={set('code')} inputMode="numeric" autoComplete="one-time-code" maxLength={6} />
            <Notice kind="error">{error}</Notice>
            <Button type="submit" variant="primary" busy={busy} disabled={form.code.trim().length !== 6}>
              Confirm
            </Button>
          </form>
        )}

        {step === 'codes' && codes && (
          <div className="stack">
            <Notice kind="warn">Save these recovery codes somewhere safe. Each one works once if you lose your authenticator. They are shown only now.</Notice>
            <ol className={`mono ${styles.codes}`}>
              {codes.list.map((c) => (
                <li key={c}>{c}</li>
              ))}
            </ol>
            <Check label="I have saved the recovery codes" checked={saved} onChange={setSaved} />
            <Button variant="primary" disabled={!saved} onClick={() => signedIn(codes.result)}>
              Continue
            </Button>
          </div>
        )}

        {step === 'mfa' && (
          <form onSubmit={verify} className="stack">
            {useRecovery ? (
              <TextInput label="Recovery code" value={form.recovery} onChange={set('recovery')} autoComplete="off" className="input mono" />
            ) : (
              <TextInput label="Code from your authenticator app" value={form.code} onChange={set('code')} inputMode="numeric" autoComplete="one-time-code" maxLength={6} />
            )}
            <Notice kind="error">{error}</Notice>
            <Button type="submit" variant="primary" busy={busy}>
              Sign in
            </Button>
            <button type="button" className="btn btn-link" onClick={() => setUseRecovery((v) => !v)}>
              {useRecovery ? 'Use the authenticator code' : 'Lost your authenticator? Use a recovery code'}
            </button>
          </form>
        )}
      </div>
    </main>
  );
}
