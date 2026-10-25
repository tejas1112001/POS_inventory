import React, { createContext, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useError } from './ErrorContext';
import { authApi, refreshAccessToken } from '../services/api';
import { clearSession, getSession, setSession, subscribeSession, tokenSecondsLeft } from '../lib/session';
import { purgeLegacyAuthData } from '../lib/authStorage';
import { setFormatSettings } from '../lib/format';

// eslint-disable-next-line react-refresh/only-export-components -- the context is shared with usePos
export const PosContext = createContext();

// PosProvider owns the session: it starts from the refresh cookie on page
// load, keeps the in-memory access token fresh, and loads the tenant's
// formatting settings from /api/auth/me.
export const PosProvider = ({ children }) => {
  const [currentUser, setCurrentUser] = useState(null);
  const [tenant, setTenant] = useState(null);
  const [impersonation, setImpersonation] = useState(false);
  const [loading, setLoading] = useState(true);
  const { showError } = useError();
  const timerRef = useRef(null);
  const currentUserRef = useRef(null);

  const loadMe = useCallback(async () => {
    try {
      const me = await authApi.me();
      setTenant(me.tenant || null);
      setImpersonation(!!me.impersonation);
      setFormatSettings(me.tenant || {});
      if (me.user) setSession({ ...getSession(), user: me.user, permissions: me.permissions });
      return me;
    } catch {
      return null;
    }
  }, []);

  const signedOut = useCallback(() => {
    clearSession();
    setTenant(null);
    setImpersonation(false);
  }, []);

  // Keep React state in step with the session store (refreshes update it).
  useEffect(() => subscribeSession((s) => setCurrentUser(s.user)), []);

  // Refresh one minute before the access token expires.
  const schedule = useCallback(() => {
    clearTimeout(timerRef.current);
    if (!getSession().token) return;
    const delay = Math.max((tokenSecondsLeft() - 60) * 1000, 15_000);
    timerRef.current = setTimeout(async () => {
      const ok = await refreshAccessToken();
      if (!ok) {
        signedOut();
        return;
      }
      schedule();
    }, delay);
  }, [signedOut]);

  useEffect(() => subscribeSession(() => schedule()), [schedule]);

  useEffect(() => {
    let active = true;
    purgeLegacyAuthData();
    (async () => {
      const body = await refreshAccessToken();
      if (body && active) await loadMe();
      if (active) setLoading(false);
    })();

    const onUnauthorized = () => {
      const wasSignedIn = !!currentUserRef.current;
      currentUserRef.current = null;
      signedOut();
      if (wasSignedIn) showError('Your session has ended. Please sign in again.');
    };
    // A tab in the background may sleep through its timer.
    const onVisible = () => {
      if (document.visibilityState === 'visible' && getSession().token && tokenSecondsLeft() < 90) {
        refreshAccessToken().then((ok) => (ok ? schedule() : signedOut()));
      }
    };
    window.addEventListener('unauthorized', onUnauthorized);
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      active = false;
      window.removeEventListener('unauthorized', onUnauthorized);
      document.removeEventListener('visibilitychange', onVisible);
      clearTimeout(timerRef.current);
    };
    // Run once on mount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    currentUserRef.current = currentUser;
  }, [currentUser]);

  // login takes the body of POST /api/auth/login (the cookie is already set).
  const login = useCallback(
    async (body) => {
      setSession({ token: body.token, user: body.user, permissions: body.permissions });
      await loadMe();
    },
    [loadMe]
  );

  const logout = useCallback(async () => {
    await authApi.logout();
    signedOut();
  }, [signedOut]);

  const value = useMemo(
    () => ({
      currentUser,
      tenant,
      impersonation,
      loading,
      login,
      logout,
      reloadMe: loadMe,
      refreshSession: refreshAccessToken,
    }),
    [currentUser, tenant, impersonation, loading, login, logout, loadMe]
  );

  return <PosContext.Provider value={value}>{children}</PosContext.Provider>;
};
