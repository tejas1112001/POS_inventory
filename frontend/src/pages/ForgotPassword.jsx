import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Loader2, Mail } from 'lucide-react';
import AuthLayout, { FormMessage } from '@/components/AuthLayout';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { api } from '@/services/api';
import { getSavedEmail } from '@/lib/authStorage';

// ForgotPassword asks for a reset link. The answer is the same whether or
// not the email has an account (POST /api/auth/password/forgot → 202).
export default function ForgotPassword() {
  const [email, setEmail] = useState(getSavedEmail());
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState(false);
  const [error, setError] = useState('');

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await api.post('/api/auth/password/forgot', { email }, { quiet: true });
      setSent(true);
    } catch (err) {
      setError(err.code === 'RATE_LIMITED' ? 'Too many requests. Try again in a few minutes.' : err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthLayout title="Reset your password" subtitle="We will email you a link to set a new password.">
      <FormMessage>{error}</FormMessage>
      {sent ? (
        <FormMessage kind="ok">
          If {email} has an account here, a reset link is on its way. The link works once and expires soon.
        </FormMessage>
      ) : (
        <form onSubmit={submit} className="mt-5 space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="email">Email</Label>
            <div className="relative">
              <Mail className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                id="email"
                type="email"
                className="pl-8"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                autoComplete="email"
              />
            </div>
          </div>
          <Button type="submit" className="h-11 w-full" disabled={busy}>
            {busy && <Loader2 className="size-4 animate-spin" />}
            Send reset link
          </Button>
        </form>
      )}
      <p className="mt-5 text-center text-sm">
        <Link to="/signin" className="font-medium text-primary hover:underline">
          Back to sign in
        </Link>
      </p>
    </AuthLayout>
  );
}
