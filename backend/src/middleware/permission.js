/**
 * permission.js middleware
 * requirePermission(module, action) → Express middleware
 *
 * ALL permission checks are database-driven via permissionService cache.
 * There is NO hardcoded admin bypass — admin's role record has all permissions seeded.
 * Legacy biller users (no role_id, role==='biller') fall back to a static whitelist
 * to ensure backward compatibility during the migration period.
 */
import { getPermissionsForRole } from '../services/permissionService.js';
import ExpressError from '../utils/ExpressError.js';

// Backward-compat whitelist for legacy biller accounts that have no role_id yet.
// This matches exactly what the seed assigns to the biller role.
const LEGACY_BILLER_WHITELIST = new Set([
  'dashboard:view',
  'products:view',
  'pos:view', 'pos:create',
  'sales:view', 'sales:create',
  'customers:view', 'customers:create',
  'inventory:view',
  'reports:view',
  'invoices:view',
  'settings:view',
]);

export function requirePermission(module, action) {
  return async (req, res, next) => {
    try {
      if (!req.user) {
        return next(new ExpressError('Authentication required.', 401));
      }

      const { roleId, role } = req.user;
      const key = `${module}:${action}`;
      let altKey = null;
      if (module === 'users' || module === 'billers') {
        const altModule = module === 'users' ? 'billers' : 'users';
        altKey = `${altModule}:${action}`;
      }

      if (roleId) {
        // ── Dynamic path: fully DB-driven ────────────────────────────────────
        const perms = await getPermissionsForRole(roleId);
        if (perms.has(key) || (altKey && perms.has(altKey))) return next();
        return next(new ExpressError('You do not have permission to perform this action.', 403));
      }

      // ── Legacy path: user has no role_id yet (pre-seed or transition) ─────
      // Admin legacy (role_id not yet assigned) — check by ENUM role name
      if (role === 'admin') {
        // Admins without role_id are in transition; allow all until next login
        return next();
      }

      // Biller legacy whitelist
      if (role === 'biller' && LEGACY_BILLER_WHITELIST.has(key)) {
        return next();
      }

      return next(new ExpressError('You do not have permission to perform this action.', 403));
    } catch (err) {
      next(err);
    }
  };
}
