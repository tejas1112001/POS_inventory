import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { api } from '@/services/api';

// usePagedList loads one page of a list from the server
// (GET endpoint?page=&limit=&search=&<filters>). The page, page size,
// search and filters live in the address (?q=&page=…), so a filtered view
// can be bookmarked and survives a reload.
export default function usePagedList(endpoint, { filters = [], pageSizes = [10, 25, 50], defaultSize = 25 } = {}) {
  const [params, setParams] = useSearchParams();
  const page = Math.max(1, Number(params.get('page')) || 1);
  const size = pageSizes.includes(Number(params.get('size'))) ? Number(params.get('size')) : defaultSize;
  const search = params.get('q') || '';
  const values = Object.fromEntries(filters.map((f) => [f, params.get(f) || '']));
  const valuesKey = JSON.stringify(values);

  const [state, setState] = useState({ rows: [], total: 0, loading: true, error: '' });
  const [tick, setTick] = useState(0);
  const live = useRef(0);

  const query = useMemo(() => {
    const q = new URLSearchParams({ page: String(page), limit: String(size) });
    if (search.trim()) q.set('search', search.trim());
    for (const [k, v] of Object.entries(JSON.parse(valuesKey))) if (v) q.set(k, v);
    return q.toString();
  }, [page, size, search, valuesKey]);

  useEffect(() => {
    const id = ++live.current;
    setState((s) => ({ ...s, loading: true, error: '' }));
    const t = setTimeout(() => {
      api
        .get(`${endpoint}${endpoint.includes('?') ? '&' : '?'}${query}`, { quiet: true })
        .then((res) => {
          if (id !== live.current) return;
          const rows = Array.isArray(res) ? res : res?.data || [];
          const total = Array.isArray(res) ? res.length : res?.pagination?.total ?? rows.length;
          setState({ rows, total, loading: false, error: '' });
        })
        .catch((err) => id === live.current && setState({ rows: [], total: 0, loading: false, error: err.message || 'Could not load the list.' }));
    }, search ? 250 : 0); // wait for typing to pause
    return () => clearTimeout(t);
  }, [endpoint, query, tick, search]);

  const update = useCallback(
    (patch, resetPage = true) => {
      setParams(
        () => {
          // Start from the address as it is now, not as it was at the last
          // render: two quick changes (a tile, then typing) must both stick.
          const next = new URLSearchParams(window.location.search);
          for (const [k, v] of Object.entries(patch)) {
            if (v === '' || v === null || v === undefined) next.delete(k);
            else next.set(k, String(v));
          }
          if (resetPage) next.delete('page');
          return next;
        },
        { replace: true }
      );
    },
    [setParams]
  );

  return {
    ...state,
    page,
    size,
    pageSizes,
    search,
    values,
    setSearch: (v) => update({ q: v }),
    setFilter: (k, v) => update({ [k]: v }),
    clearFilters: () => update(Object.fromEntries([['q', ''], ...filters.map((f) => [f, ''])])),
    setPage: (p) => update({ page: p > 1 ? p : '' }, false),
    setSize: (s) => update({ size: s === defaultSize ? '' : s }),
    reload: () => setTick((t) => t + 1),
  };
}
