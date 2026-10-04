# Dynamic Role & Permission System — Documentation

## Overview
The POS & Inventory system features a **fully dynamic, database-driven Role & Permission Management system**.
Permissions are defined per module and action, assigned to roles, and checked dynamically across both the backend (Express API endpoints) and frontend (React components, navigation menus, action buttons, and route guards).

---

## 1. Database Schema

### `roles` Table
Stores named user roles. System roles (`admin`, `biller`) have `is_system = true` to protect against deletion.
- `id` (INTEGER, PK)
- `name` (STRING, Unique)
- `description` (STRING, Nullable)
- `is_system` (BOOLEAN, default `false`)
- `createdAt`, `updatedAt`

### `permissions` Table
Lookup table of system permissions. Each permission is a `module` × `action` pair.
- `id` (INTEGER, PK)
- `module` (STRING, e.g. `products`, `sales`, `purchases`, `roles`)
- `action` (STRING, e.g. `view`, `create`, `edit`, `delete`, `approve`)

### `role_permissions` Table
Junction table linking roles to granted permissions.
- `role_id` (INTEGER, FK -> roles.id)
- `permission_id` (INTEGER, FK -> permissions.id)
- Composite Primary Key `(role_id, permission_id)`

### `users` Table Updates
- Added `role_id` (INTEGER, FK -> roles.id, Nullable)
- Preserved legacy `role` ENUM (`'admin'`, `'biller'`) for backward compatibility with existing tokens.

---

## 2. Backend Security Architecture

### Idempotent Permission Seeding (`permissionSeed.js`)
On backend startup, `seedPermissions()` automatically seeds:
- All **52 system permissions** across 17 modules (`products`, `sales`, `purchases`, `customers`, `suppliers`, `categories`, `brands`, `units`, `inventory`, `stock_adjustments`, `sales_returns`, `reports`, `invoices`, `users`, `stores`, `roles`, `pos`).
- System roles:
  - **Admin**: Assigned **all 52 permissions** in the database. No hardcoded code bypass is used; Admin permissions are resolved dynamically from the database.
  - **Biller**: Assigned read/create access appropriate for POS & sales operations, including read-only access to products (`products:view`).
- Links existing users to their respective `role_id`.

### Middleware Guards

#### `authenticateToken` (`middleware/auth.js`)
- Verifies JWT tokens and attaches `req.user = { userId, role, roleId }` to Express requests.

#### `requirePermission(module, action)` (`middleware/permission.js`)
- Protects API endpoints by checking if `req.user.roleId` has the requested `${module}:${action}` permission string.
- Uses `permissionService.js` in-memory cache (60-second TTL, automatically invalidated on role permission updates).
- All GET, POST, PUT, DELETE endpoints across all modules enforce `requirePermission(module, action)`. Direct API calls without valid permissions return HTTP `403 Forbidden`.

---

## 3. API Endpoints for Role Management

- `GET /api/roles`: Returns all roles, user counts, and assigned permission strings.
- `GET /api/roles/permissions`: Returns all available system permissions grouped by module.
- `POST /api/roles`: Creates a new custom role with assigned permission IDs.
- `PUT /api/roles/:id`: Updates a role name/description and updates its assigned permissions. Invalidates permission cache instantly.
- `DELETE /api/roles/:id`: Deletes a custom role (blocked for system roles or roles with assigned users).
- `POST /api/roles/assign-user`: Assigns a role to a user.
- `GET /api/auth/permissions`: Returns current authenticated user's permission array for real-time frontend updates.

---

## 4. Frontend Security & Dynamic UI Controls

### Permission Context (`PermissionContext.jsx`)
- Wraps the application and maintains real-time user permissions array in state and local storage.
- Polls `/api/auth/permissions` every 30 seconds to pick up administrative role updates dynamically without requiring user re-login.

### React Hook `usePermission()`
Exposes:
- `hasPermission(module, action)`: Returns `true` if the user possesses the specified permission.
- `canAccessMenu(menuKey)`: Returns `true` if user can access a specific navigation menu item.
- `refreshPermissions()`: Force-refreshes permission state from the backend.

### Automatic UI Gating
- **Sidebar & Navigation Bar**: Menus and sections are dynamically filtered using `canAccessMenu(menuKey)`. Unauthorized menus are completely hidden.
- **Action Buttons**: Edit, Delete, and Add buttons in list tables and form pages are automatically disabled or hidden using `hasPermission(module, action)`.
- **Route Protection**: `ProtectedRoute` accepts `module` and `action` props. Navigating directly via URL to unauthorized pages immediately redirects the user to `/` (Dashboard).

---

## 5. Permission Reference Matrix

| Module | Actions Available | Default Biller | Default Admin |
|---|---|---|---|
| `dashboard` | `view` | Yes | All |
| `pos` | `view`, `create`, `edit`, `delete` | `view`, `create` | All |
| `products` | `view`, `create`, `edit`, `delete` | `view` (Read-only) | All |
| `sales` | `view`, `create`, `edit`, `delete` | `view`, `create` | All |
| `purchases` | `view`, `create`, `edit`, `delete` | None | All |
| `customers` | `view`, `create`, `edit`, `delete` | `view`, `create` | All |
| `suppliers` | `view`, `create`, `edit`, `delete` | None | All |
| `categories` | `view`, `create`, `edit`, `delete` | None | All |
| `brands` | `view`, `create`, `edit`, `delete` | None | All |
| `units` | `view`, `create`, `edit`, `delete` | None | All |
| `inventory` | `view`, `create`, `edit`, `delete` | None | All |
| `stock_adjustments` | `view`, `create`, `edit`, `delete` | None | All |
| `sales_returns` | `view`, `create`, `edit`, `delete` | None | All |
| `reports` | `view`, `create`, `edit`, `delete` | None | All |
| `invoices` | `view`, `create`, `edit`, `delete` | `view` | All |
| `users` | `view`, `create`, `edit`, `delete` | None | All |
| `stores` | `view`, `create`, `edit`, `delete` | None | All |
| `roles` | `view`, `create`, `edit`, `delete` | None | All |
