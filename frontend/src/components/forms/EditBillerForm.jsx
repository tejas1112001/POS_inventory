import React, { useState, useEffect } from 'react';
import { api } from '../../services/api';

export default function EditBillerForm({ biller, onSaved, onCancel }) {
  const [name, setName] = useState(biller?.name || '');
  const [email, setEmail] = useState(biller?.email || '');
  const [roleId, setRoleId] = useState(biller?.role_id || '');
  const [status, setStatus] = useState(biller?.status || 'Active');
  const [roles, setRoles] = useState([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);

  useEffect(() => {
    api.get('/api/roles')
      .then((data) => {
        const list = Array.isArray(data) ? data : [];
        setRoles(list);
        if (!roleId && list.length > 0) {
          const matching = list.find((r) => r.name.toLowerCase() === (biller?.role || 'biller').toLowerCase());
          if (matching) setRoleId(matching.id);
        }
      })
      .catch(() => {});
  }, [biller, roleId]);

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!name.trim()) {
      setError('Name is required.');
      return;
    }

    try {
      setSaving(true);
      setError(null);
      const selectedRoleObj = roles.find((r) => r.id === parseInt(roleId));
      const legacyRole = selectedRoleObj
        ? selectedRoleObj.name.toLowerCase() === 'admin'
          ? 'admin'
          : 'biller'
        : biller?.role;

      await api.put(`/api/billers/${biller.id}`, {
        name: name.trim(),
        email: email.trim(),
        role: legacyRole,
        role_id: roleId ? parseInt(roleId) : undefined,
        status,
      });
      onSaved();
      onCancel();
    } catch (err) {
      setError(err?.message || 'Failed to update user');
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="space-y-4 p-2">
      <h3 className="text-lg font-bold text-gray-900 border-b pb-2">Edit User</h3>

      {error && <p className="text-sm text-red-600 bg-red-50 p-2 rounded">{error}</p>}

      <div>
        <label className="block text-sm font-medium text-gray-700 mb-1">
          Full Name <span className="text-red-500">*</span>
        </label>
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="w-full px-3 py-2 border rounded-lg focus:ring-blue-500 focus:border-blue-500"
          required
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-gray-700 mb-1">Email Address</label>
        <input
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          className="w-full px-3 py-2 border rounded-lg focus:ring-blue-500 focus:border-blue-500"
          required
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-gray-700 mb-1">Assigned Role</label>
        <select
          value={roleId}
          onChange={(e) => setRoleId(e.target.value)}
          className="w-full px-3 py-2 border rounded-lg focus:ring-blue-500 focus:border-blue-500 capitalize"
        >
          {roles.map((r) => (
            <option key={r.id} value={r.id}>
              {r.name} {r.is_system ? '(System)' : ''}
            </option>
          ))}
        </select>
      </div>

      <div>
        <label className="block text-sm font-medium text-gray-700 mb-1">Status</label>
        <select
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          className="w-full px-3 py-2 border rounded-lg focus:ring-blue-500 focus:border-blue-500"
        >
          <option value="Active">Active</option>
          <option value="Inactive">Inactive</option>
        </select>
      </div>

      <div className="flex justify-end gap-3 pt-3 border-t">
        <button
          type="button"
          onClick={onCancel}
          className="px-4 py-2 border border-gray-300 rounded-lg text-gray-700 hover:bg-gray-50"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={saving}
          className="px-4 py-2 bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 disabled:opacity-50"
        >
          {saving ? 'Saving...' : 'Save User'}
        </button>
      </div>
    </form>
  );
}
