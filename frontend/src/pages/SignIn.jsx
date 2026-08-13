import React, { useState, useEffect } from 'react';
import { Eye, EyeOff, ShoppingCart, Mail, Lock, Loader2 } from 'lucide-react';
import { Link, useNavigate, useLocation } from 'react-router-dom';
import { api } from '../services/api';
import { usePos } from '../hooks/usePos';
import { getSavedEmail, setSavedEmail, getRememberSession, setRememberSession } from '../lib/authStorage';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Checkbox } from '@/components/ui/checkbox';

export default function SignIn() {
  const { login } = usePos();
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

  const from = location.state?.from?.pathname || '/';

  useEffect(() => {
    const saved = getSavedEmail();
    if (saved) {
      setCredentials((prev) => ({ ...prev, email: saved }));
    }
  }, []);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setIsLoading(true);
    setError(null);
    try {
      const response = await api.post('/api/auth/login', {
        ...credentials,
        remember,
      });
      login(response);

      if (remember) {
        setSavedEmail(credentials.email);
      } else {
        setSavedEmail('');
      }

      navigate(from, { replace: true });
    } catch (err) {
      console.error('Login failed:', err);
      setError(err.message || 'Login failed. Please check your credentials.');
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-gradient-to-br from-sky-50 via-background to-blue-50 p-4">
      <div className="w-full max-w-md">
        <div className="mb-6 flex flex-col items-center text-center">
          <div className="mb-3 flex size-14 items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-lg shadow-primary/20">
            <ShoppingCart className="size-7" />
          </div>
          <h1 className="text-2xl font-bold tracking-tight">ReactronPOS</h1>
          <p className="text-sm text-muted-foreground">Retail POS & Inventory Management</p>
        </div>

        <div className="rounded-xl border bg-card p-6 shadow-sm">
          <h2 className="text-lg font-semibold">Sign in to your account</h2>
          <p className="mt-1 text-sm text-muted-foreground">Enter your credentials to continue.</p>

          {error && (
            <div className="mt-4 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}

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
              <button type="button" className="text-xs font-medium text-primary hover:underline">
                Forgot password?
              </button>
            </div>

            <Button type="submit" className="h-11 w-full" disabled={isLoading}>
              {isLoading && <Loader2 className="size-4 animate-spin" />}
              {isLoading ? 'Signing in...' : 'Sign In'}
            </Button>
          </form>

          <p className="mt-5 text-center text-sm text-muted-foreground">
            Don&apos;t have an account?{' '}
            <Link to="/signup" className="font-medium text-primary hover:underline">
              Sign Up
            </Link>
          </p>
        </div>
      </div>
    </div>
  );
}
