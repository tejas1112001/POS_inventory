import { useCallback, useEffect, useState } from 'react';
import { api } from '@/services/api';

// The shop's settings (GET /api/settings: business profile, tax and
// receipt texts), loaded once and shared. saveShopSettings replaces the
// cached copy so every screen sees the change.
let cached = null;
let inflight = null;
const listeners = new Set();

function publish(value) {
  cached = value;
  listeners.forEach((fn) => fn(value));
}

export function loadShopSettings(force = false) {
  if (cached && !force) return Promise.resolve(cached);
  if (!inflight) {
    inflight = api
      .get('/api/settings', { quiet: true })
      .then((s) => {
        publish(s);
        return s;
      })
      .finally(() => {
        inflight = null;
      });
  }
  return inflight;
}

export function setShopSettings(value) {
  publish(value);
}

export function useShopSettings() {
  const [settings, setSettings] = useState(cached);
  const [error, setError] = useState(null);
  useEffect(() => {
    listeners.add(setSettings);
    if (!cached) loadShopSettings().catch(setError);
    return () => listeners.delete(setSettings);
  }, []);
  const reload = useCallback(() => loadShopSettings(true).catch(setError), []);
  return { settings, error, reload };
}
