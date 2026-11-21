// GST (docs/M3_GST_DESIGN.md): the state codes come from the server
// (GET /api/gst/states, a fixed list), loaded once per page load.
import { useEffect, useState } from 'react';
import { api } from '@/services/api';

let cache = null;
let pending = null;

function loadStates() {
  if (cache) return Promise.resolve(cache);
  if (!pending) {
    pending = api
      .get('/api/gst/states', { quiet: true })
      .then((list) => {
        cache = Array.isArray(list) ? list : [];
        return cache;
      })
      .finally(() => {
        pending = null;
      });
  }
  return pending;
}

// useGstStates is the state list ([{ code, name, ut }]), or null while it loads.
export function useGstStates() {
  const [states, setStates] = useState(cache);
  useEffect(() => {
    if (cache) return undefined;
    let live = true;
    loadStates()
      .then((s) => live && setStates(s))
      .catch(() => live && setStates([]));
    return () => {
      live = false;
    };
  }, []);
  return states;
}

// stateLabel is "Maharashtra (27)", or the code when the list is not loaded.
export function stateLabel(states, code) {
  if (!code) return '';
  const s = (states || []).find((x) => x.code === code);
  return s ? `${s.name} (${s.code})` : code;
}

// GSTIN_RULE is the form check before the server's full one (check character, state).
export const GSTIN_RULE = { pattern: { value: /^\s*[0-9]{2}[A-Za-z0-9]{13}\s*$/, message: 'A GSTIN has 15 characters and starts with the 2-digit state code.' } };
