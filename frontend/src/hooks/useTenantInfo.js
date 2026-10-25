import { useEffect, useState } from 'react';
import { api } from '@/services/api';

let cached = null;

// useTenantInfo loads the shop's public name and logo for the sign-in
// pages (GET /api/public/tenant-info, no session needed).
export function useTenantInfo() {
  const [info, setInfo] = useState(cached);
  useEffect(() => {
    if (cached) return;
    let active = true;
    api
      .get('/api/public/tenant-info', { quiet: true })
      .then((res) => {
        cached = { name: res?.name || '', logoUrl: typeof res?.logoUrl === 'string' && res.logoUrl.startsWith('/') ? res.logoUrl : null };
        if (active) setInfo(cached);
      })
      .catch(() => {
        if (active) setInfo({ name: '', logoUrl: null });
      });
    return () => {
      active = false;
    };
  }, []);
  return info;
}
