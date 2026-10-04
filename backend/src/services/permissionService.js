/**
 * permissionService.js
 * In-memory cache for role permission sets.
 * Cache TTL: 60 seconds. Invalidated on role permission updates.
 */
import { Role, Permission } from '../models/index.js';

const cache = new Map(); // roleId (number) → { perms: Set<string>, expiresAt: number }
const CACHE_TTL = 60_000; // 60 seconds

/**
 * Returns a Set of "module:action" strings for the given roleId.
 * Uses cache; falls back to DB on miss or expiry.
 */
export async function getPermissionsForRole(roleId) {
  if (!roleId) return new Set();

  const now = Date.now();
  const cached = cache.get(roleId);
  if (cached && cached.expiresAt > now) return cached.perms;

  const role = await Role.findByPk(roleId, {
    include: [{
      model: Permission,
      as: 'permissions',
      attributes: ['module', 'action'],
      through: { attributes: [] },
    }],
  });

  const perms = new Set(
    (role?.permissions || []).map((p) => `${p.module}:${p.action}`)
  );

  cache.set(roleId, { perms, expiresAt: now + CACHE_TTL });
  return perms;
}

/**
 * Invalidate cache for a specific role.
 * Call after updating role permissions.
 */
export function invalidateRoleCache(roleId) {
  cache.delete(roleId);
}

/**
 * Invalidate the entire cache (e.g. after bulk permission changes).
 */
export function invalidateAllCache() {
  cache.clear();
}
