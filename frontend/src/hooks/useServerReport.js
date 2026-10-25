import { useEffect, useMemo, useState } from 'react';
import { api } from '@/services/api';

// queryString keeps only the filters that are set.
export function queryString(params) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params || {})) {
    if (v !== undefined && v !== null && v !== '' && v !== 'All') q.set(k, v);
  }
  const s = q.toString();
  return s ? `?${s}` : '';
}

// useServerReport loads a report with its filters applied by the server
// (dates are whole days in the shop's time zone).
export function useServerReport(path, params) {
  const qs = queryString(params);
  const [state, setState] = useState({ rows: [], loading: true, error: '' });
  useEffect(() => {
    let active = true;
    setState((s) => ({ ...s, loading: true }));
    api
      .get(`${path}${qs}`, { quiet: true })
      .then((rows) => active && setState({ rows: Array.isArray(rows) ? rows : rows?.data || [], loading: false, error: '' }))
      .catch((err) => active && setState({ rows: [], loading: false, error: err.message }));
    return () => {
      active = false;
    };
  }, [path, qs]);
  return useMemo(() => ({ ...state, qs }), [state, qs]);
}
