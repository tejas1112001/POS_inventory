import { useCallback, useMemo } from 'react';
import { usePos } from './usePos';

// useModules: the shop's business type and the industry modules it can use
// (switched on and in its plan), from /api/auth/me (industry P1 item 1).
// Only for showing and hiding: the server refuses a module that is off
// with 403 MODULE_OFF.
export function useModules() {
  const { tenant, reloadMe } = usePos();
  const modules = useMemo(() => tenant?.modules || [], [tenant]);
  const hasModule = useCallback((key) => modules.includes(key), [modules]);
  return {
    businessType: tenant?.businessType || 'retail',
    modules,
    entitledModules: tenant?.entitledModules || [],
    hasModule,
    reload: reloadMe,
  };
}
