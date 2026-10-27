import React, { useState, useEffect } from 'react';
import { Eye, EyeOff, Mail, Lock, Loader2 } from 'lucide-react';
import { Link, useNavigate, useLocation } from 'react-router-dom';
import { authApi } from '../services/api';
import AuthLayout, { FormMessage } from '@/components/AuthLayout';
import { usePos } from '../hooks/usePos';
import { getSavedEmail, setSavedEmail, getRememberSession, setRememberSession } from '../lib/authStorage';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Checkbox } from '@/components/ui/checkbox';

export default function SignIn() {
  const { login, currentUser, loading } = usePos();
  const navigate = useNavigate();
  const location = useLocation();
  const [credentials, setCredentials] = useState({
    email: getSavedEmail(),
    password: '',
  });
  const [showPw, setShowPw] = useState(false);
  const [remember, setRemember] = useState(getRememberSession());
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState(null);

  const from = location.state?.from?.pathname || '/dashboard';

  // Already signed in (session restored from the cookie): go on.
  useEffect(() => {
    if (!loading && currentUser) navigate(from, { replace: true });
  }, [loading, currentUser, from, navigate]);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setIsLoading(true);
    setError(null);
    try {
      const response = await authApi.login(credentials.email, credentials.password, remember);
      await login(response);

      if (remember) {
        setSavedEmail(credentials.email);
      } else {
        setSavedEmail('');
      }

      navigate(from, { replace: true });
    } catch (err) {
      setError(signInMessage(err));
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <AuthLayout title="Sign in" subtitle="Enter your email and password.">
      <FormMessage>{error}</FormMessage>

      <form onSubmit={handleSubmit} className="mt-5 space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor="email">Email</Label>
          <div className="relative">
            <Mail className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              id="email"
              type="email"
              value={credentials.email}
              onChange={(e) => setCredentials({ ...credentials, email: e.target.value })}
              className="pl-8"
              placeholder="you@store.com"
              required
              autoComplete="email"
            />
          </div>
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="password">Password</Label>
          <div className="relative">
            <Lock className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              id="password"
              type={showPw ? 'text' : 'password'}
              value={credentials.password}
              onChange={(e) => setCredentials({ ...credentials, password: e.target.value })}
              className="px-8"
              placeholder="••••••••"
              required
              autoComplete={remember ? 'current-password' : 'off'}
            />
            <button
              type="button"
              onClick={() => setShowPw((v) => !v)}
              className="absolute right-2 top-1/2 -translate-y-1/2 rounded-sm p-0.5 text-muted-foreground hover:text-foreground"
              aria-label={showPw ? 'Hide password' : 'Show password'}
            >
              {showPw ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
            </button>
          </div>
        </div>

        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Checkbox
              id="remember"
              checked={remember}
              onCheckedChange={(v) => {
                const val = !!v;
                setRemember(val);
                setRememberSession(val);
              }}
            />
            <Label htmlFor="remember" className="cursor-pointer text-sm font-normal">
              Remember me
            </Label>
          </div>
          <Link to="/forgot-password" className="text-xs font-medium text-primary hover:underline">
            Forgot password?
          </Link>
        </div>

        <Button type="submit" className="h-11 w-full" disabled={isLoading}>
          {isLoading && <Loader2 className="size-4 animate-spin" />}
          {isLoading ? 'Signing in...' : 'Sign In'}
        </Button>
      </form>
    </AuthLayout>
  );
}

// signInMessage turns the API's error codes into plain sentences.
function signInMessage(err) {
  switch (err?.code) {
    case 'INVALID_CREDENTIALS':
      return 'The email or password is not correct.';
    case 'ACCOUNT_LOCKED':
    case 'RATE_LIMITED': {
      const mins = err.retryAfter ? Math.ceil(err.retryAfter / 60) : null;
      return mins
        ? `Too many attempts. Try again in ${mins} minute${mins === 1 ? '' : 's'}.`
        : 'Too many attempts. Try again in a few minutes.';
    }
    case 'TENANT_SUSPENDED':
      return 'This shop is suspended. The owner has been emailed about it.';
    case 'NETWORK':
      return err.message;
    default:
      return err?.message || 'Sign-in failed. Please try again.';
  }
}
