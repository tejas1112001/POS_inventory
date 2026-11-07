import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { api, clearSession, getSession, onSession, refresh, setSession } from './api';

const SessionContext = createContext(null);

// SessionProvider restores the session from the refresh cookie on load,
// refreshes the short access token before it expires, and signs out.
export function SessionProvider({ children }) {
  const [state, setState] = useState({ loading: true, user: null });
  const timer = useRef(null);

  useEffect(() => onSession((s) => setState((prev) => ({ ...prev, user: s.user }))), []);

  const schedule = useCallback(() => {
    clearTimeout(timer.current);
    const s = getSession();
    if (!s.token) return;
    const wait = Math.max(s.expiresAt - Date.now() - 60_000, 15_000);
    timer.current = setTimeout(async () => {
      if (await refresh()) schedule();
      else clearSession();
    }, wait);
  }, []);

  useEffect(() => onSession(() => schedule()), [schedule]);

  useEffect(() => {
    let active = true;
    refresh().finally(() => active && setState((prev) => ({ ...prev, loading: false })));
    const wake = () => {
      const s = getSession();
      if (document.visibilityState === 'visible' && s.token && s.expiresAt - Date.now() < 90_000) refresh().then((ok) => !ok && clearSession());
    };
    document.addEventListener('visibilitychange', wake);
    return () => {
      active = false;
      document.removeEventListener('visibilitychange', wake);
      clearTimeout(timer.current);
    };
  }, []);

  const signedIn = useCallback((res) => setSession(res), []);
  const signOut = useCallback(async () => {
    try {
      await api.post('/auth/logout');
    } catch {
      /* the cookie may already be gone */
    }
    clearSession();
  }, []);

  const value = useMemo(() => {
    const perms = new Set(state.user?.permissions || []);
    return { ...state, signedIn, signOut, can: (p) => perms.has(p) };
  }, [state, signedIn, signOut]);

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

// eslint-disable-next-line react-refresh/only-export-components -- hook kept with its provider
export function useSession() {
  return useContext(SessionContext);
}
