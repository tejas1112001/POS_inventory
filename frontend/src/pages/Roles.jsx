import { useConfirm } from '../components/feedback/ConfirmDialog';
import { toast } from '../../shared/toast/store';
import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { ShieldCheck, Plus, Edit, Trash2, Users, CheckCircle2 } from 'lucide-react';
import ListPageShell from '../components/layout/ListPageShell';
import ListPageLayout from '../components/ListComponents/ListPageLayout';
import ListControlButtons from '../components/ListComponents/ListControlButtons';
import FilterBar from '@/components/kit/FilterBar';
import { Button } from '@/components/ui/button';
import { usePermission } from '../hooks/usePermission';
import { api } from '../services/api';

export default function Roles() {
  const [roles, setRoles] = useState([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [error, setError] = useState(null);
  const navigate = useNavigate();
  const { hasPermission, refreshPermissions } = usePermission();
  const confirm = useConfirm();

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
      toast.error('System roles cannot be deleted.');
      return;
    }
    const ok = await confirm({
      title: `Delete the role "${role.name}"?`,
      body: 'Users must be moved to another role first. This cannot be undone.',
      confirmLabel: 'Delete role',
      destructive: true,
    });
    if (!ok) return;
    try {
      await api.delete(`/api/roles/${role.id}`, { quiet: true });
      toast.success(`Role "${role.name}" deleted.`);
      fetchRoles();
      refreshPermissions();
    } catch (err) {
      toast.error(err?.message || 'The role could not be deleted.');
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
      <div className="space-y-3">
        {error && (
          <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
            {error}
          </div>
        )}

        <FilterBar
          search={search}
          onSearch={setSearch}
          placeholder="Search roles…"
          actions={
            hasPermission('roles', 'create') && (
              <Button type="button" className="max-sm:h-11" onClick={() => navigate('/add-role')}>
                <Plus className="size-4" aria-hidden="true" /> New role
              </Button>
            )
          }
        />

        {/* Roles Grid */}
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {filteredRoles.map((role) => (
            <div
              key={role.id}
              className="relative flex flex-col justify-between rounded-2xl border bg-card p-4 shadow-xs transition-shadow hover:shadow-md"
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
                    className="inline-flex items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-accent transition-colors"
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
