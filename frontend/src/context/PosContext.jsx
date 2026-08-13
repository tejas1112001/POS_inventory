import React, { createContext, useContext, useState, useEffect, useCallback, useRef } from 'react';
import { useError } from './ErrorContext';
import {
  getToken,
  getStoredUser,
  setAuthData,
  clearAuthData,
  isTokenExpired,
  getRememberSession,
} from '../lib/authStorage';

export const PosContext = createContext();

const API_BASE = import.meta.env?.VITE_API_URL?.replace(/\/+$/, '') ||
  (import.meta.env?.DEV ? 'http://localhost:3000' : '');

export const PosProvider = ({ children }) => {
  const [currentUser, setCurrentUser] = useState(null);
  const [loading, setLoading] = useState(true);
  const { showError } = useError();
  const refreshTimerRef = useRef(null);

  const logout = useCallback(() => {
    setCurrentUser(null);
    clearAuthData();
    if (refreshTimerRef.current) {
      clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = null;
    }
  }, []);

  const refreshSession = useCallback(async () => {
    const token = getToken();
    if (!token || isTokenExpired(token, 0)) return false;

    try {
      const response = await fetch(`${API_BASE}/api/auth/refresh`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ remember: getRememberSession() }),
      });

      if (!response.ok) return false;

      const data = await response.json();
      setAuthData({
        user: data.user,
        token: data.token,
        remember: data.remember ?? getRememberSession(),
      });
      setCurrentUser(data.user);
      return true;
    } catch {
      return false;
    }
  }, []);

  const scheduleRefresh = useCallback(() => {
    if (refreshTimerRef.current) {
      clearTimeout(refreshTimerRef.current);
    }

    const token = getToken();
    if (!token) return;

    const payload = JSON.parse(atob(token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')));
    const expiresAt = payload.exp * 1000;
    const refreshAt = expiresAt - 5 * 60 * 1000;
    const delay = Math.max(refreshAt - Date.now(), 30_000);

    refreshTimerRef.current = setTimeout(async () => {
      const ok = await refreshSession();
      if (ok) {
        scheduleRefresh();
      }
    }, delay);
  }, [refreshSession]);

  useEffect(() => {
    const initAuth = async () => {
      const user = getStoredUser();
      const token = getToken();

      if (user && token) {
        if (isTokenExpired(token, 0)) {
          const refreshed = await refreshSession();
          if (!refreshed) {
            clearAuthData();
          } else {
            setCurrentUser(getStoredUser());
            scheduleRefresh();
          }
        } else {
          setCurrentUser(user);
          scheduleRefresh();
        }
      }
      setLoading(false);
    };

    initAuth();

    const handleUnauthorized = async () => {
      const refreshed = await refreshSession();
      if (!refreshed) {
        showError('Your session has expired. Please sign in again.');
        logout();
      }
    };

    window.addEventListener('unauthorized', handleUnauthorized);

    return () => {
      window.removeEventListener('unauthorized', handleUnauthorized);
      if (refreshTimerRef.current) {
        clearTimeout(refreshTimerRef.current);
      }
    };
  }, [logout, refreshSession, scheduleRefresh, showError]);

  const login = (userData) => {
    const { user, token, remember = true } = userData;
    setAuthData({ user, token, remember });
    setCurrentUser(user);
    scheduleRefresh();
  };

  const value = {
    currentUser,
    login,
    logout,
    loading,
    refreshSession,
  };

  return (
    <PosContext.Provider value={value}>
      {children}
    </PosContext.Provider>
  );
};
