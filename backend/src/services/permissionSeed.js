import { Role, Permission, RolePermission, User } from '../models/index.js';
import { Op } from 'sequelize';

// ─── All permissions: module × action ─────────────────────────────────────────
const ALL_PERMISSIONS = [
  { module: 'dashboard',         action: 'view',   label: 'View Dashboard' },
  { module: 'products',          action: 'view',   label: 'View Products' },
  { module: 'products',          action: 'create', label: 'Create Products' },
  { module: 'products',          action: 'edit',   label: 'Edit Products' },
  { module: 'products',          action: 'delete', label: 'Delete Products' },
  { module: 'categories',        action: 'view',   label: 'View Categories' },
  { module: 'categories',        action: 'create', label: 'Create Categories' },
  { module: 'categories',        action: 'edit',   label: 'Edit Categories' },
  { module: 'categories',        action: 'delete', label: 'Delete Categories' },
  { module: 'brands',            action: 'view',   label: 'View Brands' },
  { module: 'brands',            action: 'create', label: 'Create Brands' },
  { module: 'brands',            action: 'edit',   label: 'Edit Brands' },
  { module: 'brands',            action: 'delete', label: 'Delete Brands' },
  { module: 'units',             action: 'view',   label: 'View Units' },
  { module: 'units',             action: 'create', label: 'Create Units' },
  { module: 'units',             action: 'edit',   label: 'Edit Units' },
  { module: 'units',             action: 'delete', label: 'Delete Units' },
  { module: 'inventory',         action: 'view',   label: 'View Inventory' },
  { module: 'inventory',         action: 'create', label: 'Create/Upsert Inventory' },
  { module: 'inventory',         action: 'edit',   label: 'Edit Inventory' },
  { module: 'stock_adjustments', action: 'view',   label: 'View Stock Adjustments' },
  { module: 'stock_adjustments', action: 'create', label: 'Create Stock Adjustments' },
  { module: 'sales',             action: 'view',   label: 'View Sales' },
  { module: 'sales',             action: 'create', label: 'Create Sales' },
  { module: 'sales',             action: 'edit',   label: 'Edit Sales' },
  { module: 'sales',             action: 'delete', label: 'Delete Sales' },
  { module: 'pos',               action: 'view',   label: 'Access POS' },
  { module: 'pos',               action: 'create', label: 'Process POS Sale' },
  { module: 'purchases',         action: 'view',   label: 'View Purchases' },
  { module: 'purchases',         action: 'create', label: 'Create Purchases' },
  { module: 'purchases',         action: 'edit',   label: 'Edit Purchases' },
  { module: 'purchases',         action: 'approve',label: 'Approve/Receive Purchases' },
  { module: 'sales_returns',     action: 'view',   label: 'View Sales Returns' },
  { module: 'sales_returns',     action: 'create', label: 'Create Sales Returns' },
  { module: 'customers',         action: 'view',   label: 'View Customers' },
  { module: 'customers',         action: 'create', label: 'Create Customers' },
  { module: 'customers',         action: 'edit',   label: 'Edit Customers' },
  { module: 'customers',         action: 'delete', label: 'Delete Customers' },
  { module: 'suppliers',         action: 'view',   label: 'View Suppliers' },
  { module: 'suppliers',         action: 'create', label: 'Create Suppliers' },
  { module: 'suppliers',         action: 'edit',   label: 'Edit Suppliers' },
  { module: 'suppliers',         action: 'delete', label: 'Delete Suppliers' },
  { module: 'billers',           action: 'view',   label: 'View Users/Billers' },
  { module: 'billers',           action: 'create', label: 'Create Users/Billers' },
  { module: 'billers',           action: 'edit',   label: 'Edit Users/Billers' },
  { module: 'billers',           action: 'delete', label: 'Delete Users/Billers' },
  { module: 'users',             action: 'view',   label: 'View Users' },
  { module: 'users',             action: 'create', label: 'Create Users' },
  { module: 'users',             action: 'edit',   label: 'Edit Users' },
  { module: 'users',             action: 'delete', label: 'Delete Users' },
  { module: 'roles',             action: 'view',   label: 'View Roles' },
  { module: 'roles',             action: 'create', label: 'Create Roles' },
  { module: 'roles',             action: 'edit',   label: 'Edit Roles' },
  { module: 'roles',             action: 'delete', label: 'Delete Roles' },
  { module: 'reports',           action: 'view',   label: 'View Reports' },
  { module: 'invoices',          action: 'view',   label: 'View Invoices' },
  { module: 'settings',          action: 'view',   label: 'View Settings' },
];

// ─── Biller permissions (read-only products:view included per approval) ────────
const BILLER_PERMISSIONS = [
  'dashboard:view',
  'products:view',          // read-only — no create/edit/delete
  'pos:view',
  'pos:create',
  'sales:view',
  'sales:create',
  'customers:view',
  'customers:create',
  'inventory:view',
  'reports:view',
  'invoices:view',
  'settings:view',
];

export async function seedPermissions() {
  try {
    // 1. Upsert all permissions (idempotent)
    for (const perm of ALL_PERMISSIONS) {
      await Permission.findOrCreate({
        where: { module: perm.module, action: perm.action },
        defaults: { label: perm.label },
      });
    }

    // Fetch all permissions as a map: "module:action" → id
    const allPerms = await Permission.findAll({ attributes: ['id', 'module', 'action'] });
    const permMap = new Map(allPerms.map((p) => [`${p.module}:${p.action}`, p.id]));

    // 2. Ensure admin role exists (is_system, all permissions)
    const [adminRole] = await Role.findOrCreate({
      where: { name: 'admin' },
      defaults: { description: 'Full system access', color: '#dc2626', is_system: true },
    });

    // Assign ALL permissions to admin
    const allPermIds = allPerms.map((p) => ({ role_id: adminRole.id, permission_id: p.id }));
    await RolePermission.destroy({ where: { role_id: adminRole.id } });
    await RolePermission.bulkCreate(allPermIds, { ignoreDuplicates: true });

    // 3. Ensure biller role exists (is_system, limited permissions)
    const [billerRole] = await Role.findOrCreate({
      where: { name: 'biller' },
      defaults: { description: 'Sales and POS access', color: '#2563eb', is_system: true },
    });

    const billerPermIds = BILLER_PERMISSIONS
      .map((key) => permMap.get(key))
      .filter(Boolean)
      .map((pid) => ({ role_id: billerRole.id, permission_id: pid }));
    await RolePermission.destroy({ where: { role_id: billerRole.id } });
    await RolePermission.bulkCreate(billerPermIds, { ignoreDuplicates: true });

    // 4. Link existing users to their role_id (migration for old users)
    await User.update({ role_id: adminRole.id }, { where: { role: 'admin', role_id: null } });
    await User.update({ role_id: billerRole.id }, { where: { role: 'biller', role_id: null } });

    console.log(`[Permissions] Seeded ${ALL_PERMISSIONS.length} permissions, roles: admin (${allPermIds.length} perms), biller (${billerPermIds.length} perms).`);
  } catch (err) {
    console.error('[Permissions] Seed failed:', err.message);
    // Non-fatal — app still starts
  }
}
