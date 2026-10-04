import React, { useState, useEffect } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { ShieldCheck, Check, ArrowLeft } from 'lucide-react';
import FormPageLayout, { FormCard, FormBody } from '../components/forms/FormPageLayout';
import FormFooter from '../components/forms/FormFooter';
import { usePermission } from '../hooks/usePermission';
import { api } from '../services/api';
import { cn } from '@/lib/utils';

export default function AddRole() {
  const { id } = useParams();
  const isEditing = Boolean(id);
  const navigate = useNavigate();
  const { refreshPermissions } = usePermission();

  const [roleName, setRoleName] = useState('');
  const [description, setDescription] = useState('');
  const [allPermissions, setAllPermissions] = useState([]);
  const [selectedPermissionIds, setSelectedPermissionIds] = useState(new Set());
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);

  useEffect(() => {
    fetchData();
  }, [id]);

  const fetchData = async () => {
    try {
      setLoading(true);
      setError(null);
      const perms = await api.get('/api/roles/permissions');
      setAllPermissions(perms || []);

      if (isEditing) {
        const roles = await api.get('/api/roles');
        const role = (roles || []).find((r) => r.id === parseInt(id));
        if (role) {
          setRoleName(role.name);
          setDescription(role.description || '');
          setSelectedPermissionIds(new Set(role.permissionIds || []));
        } else {
          setError('Role not found');
        }
      }
    } catch (err) {
      setError(err?.message || 'Failed to load role data');
    } finally {
      setLoading(false);
    }
  };

  // Group permissions by module
  const groupedPermissions = allPermissions.reduce((acc, perm) => {
    if (!acc[perm.module]) {
      acc[perm.module] = [];
    }
    acc[perm.module].push(perm);
    return acc;
  }, {});

  const togglePermission = (permId) => {
    setSelectedPermissionIds((prev) => {
      const next = new Set(prev);
      if (next.has(permId)) {
        next.delete(permId);
      } else {
        next.add(permId);
      }
      return next;
    });
  };

  const toggleModuleAll = (moduleName) => {
    const modulePerms = groupedPermissions[moduleName] || [];
    const allSelected = modulePerms.every((p) => selectedPermissionIds.has(p.id));

    setSelectedPermissionIds((prev) => {
      const next = new Set(prev);
      modulePerms.forEach((p) => {
        if (allSelected) {
          next.delete(p.id);
        } else {
          next.add(p.id);
        }
      });
      return next;
    });
  };

  const selectAll = () => {
    setSelectedPermissionIds(new Set(allPermissions.map((p) => p.id)));
  };

  const deselectAll = () => {
    setSelectedPermissionIds(new Set());
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!roleName.trim()) {
      setError('Role name is required.');
      return;
    }

    try {
      setSaving(true);
      setError(null);
      const payload = {
        name: roleName.trim(),
        description: description.trim(),
        permissionIds: Array.from(selectedPermissionIds),
      };

      if (isEditing) {
        await api.put(`/api/roles/${id}`, payload);
      } else {
        await api.post('/api/roles', payload);
      }

      await refreshPermissions();
      navigate('/roles');
    } catch (err) {
      setError(err?.message || 'Failed to save role');
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="py-20 text-center text-muted-foreground">
        Loading permissions matrix...
      </div>
    );
  }

  return (
    <FormPageLayout>
      <div className="mb-6 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <button
            onClick={() => navigate('/roles')}
            className="rounded-lg border p-2 hover:bg-accent transition-colors"
          >
            <ArrowLeft className="h-5 w-5" />
          </button>
          <div>
            <h1 className="text-2xl font-bold tracking-tight text-foreground flex items-center gap-2">
              <ShieldCheck className="h-6 w-6 text-primary" />
              {isEditing ? `Edit Role: ${roleName}` : 'Create Custom Role'}
            </h1>
            <p className="text-sm text-muted-foreground">
              Define role details and toggle menu-wise and action-wise permissions.
            </p>
          </div>
        </div>

        <div className="flex gap-2">
          <button
            type="button"
            onClick={selectAll}
            className="text-xs font-semibold px-3 py-1.5 rounded-md border hover:bg-accent transition-colors"
          >
            Select All Permissions
          </button>
          <button
            type="button"
            onClick={deselectAll}
            className="text-xs font-semibold px-3 py-1.5 rounded-md border hover:bg-accent transition-colors"
          >
            Clear All
          </button>
        </div>
      </div>

      {error && (
        <div className="mb-4 rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
          {error}
        </div>
      )}

      <form onSubmit={handleSubmit} className="flex min-h-0 flex-1 flex-col space-y-6">
        <FormCard>
          <FormBody>
            <div className="space-y-6">
              {/* Role Metadata */}
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-semibold text-foreground mb-1">
                    Role Name <span className="text-destructive">*</span>
                  </label>
                  <input
                    type="text"
                    value={roleName}
                    onChange={(e) => setRoleName(e.target.value)}
                    placeholder="e.g. Inventory Manager, Store Accountant"
                    className="w-full px-3 py-2 border rounded-lg focus:ring-2 focus:ring-primary focus:outline-none"
                    required
                  />
                </div>

                <div>
                  <label className="block text-sm font-semibold text-foreground mb-1">
                    Description
                  </label>
                  <input
                    type="text"
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    placeholder="Brief summary of role responsibilities"
                    className="w-full px-3 py-2 border rounded-lg focus:ring-2 focus:ring-primary focus:outline-none"
                  />
                </div>
              </div>

              {/* Permission Matrix */}
              <div className="border-t pt-6">
                <h3 className="text-lg font-bold text-foreground mb-4">Module Permissions Matrix</h3>
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                  {Object.entries(groupedPermissions).map(([moduleName, perms]) => {
                    const allSelected = perms.every((p) => selectedPermissionIds.has(p.id));
                    const someSelected = perms.some((p) => selectedPermissionIds.has(p.id));

                    return (
                      <div
                        key={moduleName}
                        className="rounded-lg border bg-card p-4 shadow-xs space-y-3"
                      >
                        <div className="flex items-center justify-between border-b pb-2">
                          <span className="font-bold text-sm text-card-foreground capitalize">
                            {moduleName.replace(/_/g, ' ')}
                          </span>
                          <button
                            type="button"
                            onClick={() => toggleModuleAll(moduleName)}
                            className="text-xs text-primary font-semibold hover:underline"
                          >
                            {allSelected ? 'Deselect Module' : 'Select All'}
                          </button>
                        </div>

                        <div className="space-y-1.5">
                          {perms.map((p) => {
                            const isChecked = selectedPermissionIds.has(p.id);
                            return (
                              <label
                                key={p.id}
                                className={cn(
                                  'flex items-center justify-between rounded-md border px-3 py-2 text-xs font-medium cursor-pointer transition-colors',
                                  isChecked
                                    ? 'bg-primary/10 border-primary/30 text-primary font-semibold'
                                    : 'bg-background border-border text-muted-foreground hover:bg-muted/50'
                                )}
                              >
                                <span className="capitalize">{p.action}</span>
                                <input
                                  type="checkbox"
                                  checked={isChecked}
                                  onChange={() => togglePermission(p.id)}
                                  className="h-4 w-4 rounded border-gray-300 text-primary focus:ring-primary accent-primary"
                                />
                              </label>
                            );
                          })}
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            </div>
          </FormBody>

          <FormFooter
            cancelPath="/roles"
            submitLabel={saving ? 'Saving...' : isEditing ? 'Update Role' : 'Create Role'}
            disabled={saving}
          />
        </FormCard>
      </form>
    </FormPageLayout>
  );
}
