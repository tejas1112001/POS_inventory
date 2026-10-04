import React, { createContext, useContext, useState, useEffect, useCallback } from 'react';
import { getStoredPermissions, setStoredPermissions, getToken } from '../lib/authStorage';
import { api } from '../services/api';

const PermissionContext = createContext();

export const PermissionProvider = ({ children }) => {
  const [permissions, setPermissions] = useState(() => getStoredPermissions());
  const [loadingPermissions, setLoadingPermissions] = useState(false);

  const updatePermissions = useCallback((newPerms) => {
    if (Array.isArray(newPerms)) {
      setPermissions(newPerms);
      setStoredPermissions(newPerms);
    }
  }, []);

  const refreshPermissions = useCallback(async () => {
    const token = getToken();
    if (!token) {
      setPermissions([]);
      return;
    }
    try {
      setLoadingPermissions(true);
      const res = await api.get('/api/auth/permissions');
      const fetched = res?.permissions || [];
      updatePermissions(fetched);
    } catch (err) {
      console.warn('Failed to refresh permissions:', err?.message);
    } finally {
      setLoadingPermissions(false);
    }
  }, [updatePermissions]);

  // Sync on token change or startup
  useEffect(() => {
    refreshPermissions();
  }, [refreshPermissions]);

  // Periodic background refresh every 30 seconds to pick up admin updates dynamically
  useEffect(() => {
    const interval = setInterval(() => {
      if (getToken()) {
        refreshPermissions();
      }
    }, 30000);
    return () => clearInterval(interval);
  }, [refreshPermissions]);

  const hasPermission = useCallback(
    (module, action) => {
      if (!module) return true;
      const target = action ? `${module}:${action}` : module;
      if (permissions.includes(target)) return true;
      if (module === 'users' || module === 'billers') {
        const altModule = module === 'users' ? 'billers' : 'users';
        const altTarget = action ? `${altModule}:${action}` : altModule;
        return permissions.includes(altTarget);
      }
      return false;
    },
    [permissions]
  );

  const canAccessMenu = useCallback(
    (menuKey) => {
      switch (menuKey) {
        case 'dashboard':
          return hasPermission('dashboard', 'view');
        case 'products':
          return hasPermission('products', 'view');
        case 'pos':
          return hasPermission('pos', 'view');
        case 'sales':
          return hasPermission('sales', 'view');
        case 'purchases':
          return hasPermission('purchases', 'view');
        case 'customers':
          return hasPermission('customers', 'view');
        case 'suppliers':
          return hasPermission('suppliers', 'view');
        case 'categories':
          return hasPermission('categories', 'view');
        case 'brands':
          return hasPermission('brands', 'view');
        case 'units':
          return hasPermission('units', 'view');
        case 'users':
        case 'billers':
          return hasPermission('users', 'view');
        case 'roles':
          return hasPermission('roles', 'view');
        case 'inventory':
          return hasPermission('inventory', 'view');
        case 'stock_adjustments':
          return hasPermission('stock_adjustments', 'view');
        case 'sales_returns':
          return hasPermission('sales_returns', 'view');
        case 'reports':
          return hasPermission('reports', 'view');
        case 'invoices':
          return hasPermission('invoices', 'view');
        case 'stores':
          return hasPermission('stores', 'view');
        default:
          return true;
      }
    },
    [hasPermission]
  );

  return (
    <PermissionContext.Provider
      value={{
        permissions,
        loadingPermissions,
        hasPermission,
        canAccessMenu,
        refreshPermissions,
        updatePermissions,
      }}
    >
      {children}
    </PermissionContext.Provider>
  );
};

export const usePermission = () => {
  const context = useContext(PermissionContext);
  if (!context) {
    throw new Error('usePermission must be used within a PermissionProvider');
  }
  return context;
};

export default PermissionContext;
