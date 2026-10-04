import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { ShieldCheck, Plus, Edit, Trash2, Users, CheckCircle2 } from 'lucide-react';
import ListPageShell from '../components/layout/ListPageShell';
import ListPageLayout from '../components/ListComponents/ListPageLayout';
import ListControlButtons from '../components/ListComponents/ListControlButtons';
import SearchInput from '../components/ListComponents/SearchInput';
import { usePermission } from '../hooks/usePermission';
import { api } from '../services/api';

export default function Roles() {
  const [roles, setRoles] = useState([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [error, setError] = useState(null);
  const navigate = useNavigate();
  const { hasPermission, refreshPermissions } = usePermission();

  useEffect(() => {
    fetchRoles();
  }, []);

  const fetchRoles = async () => {
    try {
      setLoading(true);
      setError(null);
      const res = await api.get('/api/roles');
      setRoles(res || []);
    } catch (err) {
      setError(err?.message || 'Failed to fetch roles');
    } finally {
      setLoading(false);
    }
  };

  const handleDeleteRole = async (role) => {
    if (role.is_system) {
      alert('System roles cannot be deleted.');
      return;
    }
    if (!window.confirm(`Are you sure you want to delete the role "${role.name}"?`)) {
      return;
    }
    try {
      await api.delete(`/api/roles/${role.id}`);
      fetchRoles();
      refreshPermissions();
    } catch (err) {
      alert(err?.message || 'Failed to delete role');
    }
  };

  const filteredRoles = roles.filter((role) =>
    role.name.toLowerCase().includes(search.toLowerCase()) ||
    (role.description && role.description.toLowerCase().includes(search.toLowerCase()))
  );

  if (loading) {
    return (
      <div className="py-20 text-center text-muted-foreground">
        Loading roles & permissions...
      </div>
    );
  }

  return (
    <ListPageShell>
      <div className="space-y-6">
        {/* Header bar */}
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
          <div>
            <h1 className="text-2xl font-bold tracking-tight text-foreground flex items-center gap-2">
              <ShieldCheck className="h-7 w-7 text-primary" />
              Role & Permission Management
            </h1>
            <p className="text-sm text-muted-foreground">
              Manage custom roles, assign module permissions, and control user access across the application.
            </p>
          </div>
          {hasPermission('roles', 'create') && (
            <button
              onClick={() => navigate('/add-role')}
              className="inline-flex items-center gap-2 rounded-lg bg-primary px-4 py-2 text-sm font-semibold text-primary-foreground shadow-sm hover:bg-primary/90 transition-all"
            >
              <Plus className="h-4 w-4" />
              Create Custom Role
            </button>
          )}
        </div>

        {error && (
          <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
            {error}
          </div>
        )}

        {/* Search */}
        <div className="w-full max-w-md">
          <SearchInput search={search} setSearch={setSearch} placeholder="Search roles..." />
        </div>

        {/* Roles Grid */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {filteredRoles.map((role) => (
            <div
              key={role.id}
              className="relative flex flex-col justify-between rounded-xl border bg-card p-6 shadow-xs hover:shadow-md transition-shadow"
            >
              <div>
                <div className="flex items-center justify-between gap-2 mb-3">
                  <div className="flex items-center gap-2">
                    <h3 className="text-lg font-bold text-card-foreground capitalize">{role.name}</h3>
                    {role.is_system && (
                      <span className="inline-flex items-center gap-1 rounded-full bg-blue-500/10 px-2.5 py-0.5 text-xs font-semibold text-blue-600 dark:text-blue-400 border border-blue-500/20">
                        System Role
                      </span>
                    )}
                  </div>
                  <span className="flex items-center gap-1 text-xs text-muted-foreground font-medium">
                    <Users className="h-3.5 w-3.5" />
                    {role.userCount} {role.userCount === 1 ? 'user' : 'users'}
                  </span>
                </div>

                <p className="text-sm text-muted-foreground mb-4 min-h-[40px]">
                  {role.description || (role.is_system ? 'Built-in system role with default permissions.' : 'Custom user role.')}
                </p>

                <div className="border-t pt-4">
                  <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                    Granted Permissions ({role.permissions?.length || 0})
                  </span>
                  <div className="mt-2 flex flex-wrap gap-1.5 max-h-32 overflow-y-auto pr-1">
                    {role.permissions && role.permissions.length > 0 ? (
                      role.permissions.map((p) => (
                        <span
                          key={p}
                          className="inline-flex items-center rounded-md bg-secondary px-2 py-0.5 text-[11px] font-medium text-secondary-foreground"
                        >
                          {p}
                        </span>
                      ))
                    ) : (
                      <span className="text-xs text-muted-foreground italic">No permissions assigned</span>
                    )}
                  </div>
                </div>
              </div>

              {/* Action Buttons */}
              <div className="mt-6 flex items-center justify-end gap-2 border-t pt-4">
                {hasPermission('roles', 'edit') && (
                  <button
                    onClick={() => navigate(`/edit-role/${role.id}`)}
                    className="inline-flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-accent transition-colors"
                  >
                    <Edit className="h-3.5 w-3.5" />
                    Edit Permissions
                  </button>
                )}

                {hasPermission('roles', 'delete') && !role.is_system && (
                  <button
                    onClick={() => handleDeleteRole(role)}
                    className="inline-flex items-center gap-1.5 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-1.5 text-xs font-medium text-destructive hover:bg-destructive hover:text-destructive-foreground transition-colors"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    Delete
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>
    </ListPageShell>
  );
}
