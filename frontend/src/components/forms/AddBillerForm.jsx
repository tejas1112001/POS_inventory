import React, { useState, useEffect } from 'react';
import { api } from '../../services/api';

export default function AddBillerForm({ onSaved, onCancel }) {
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [roleId, setRoleId] = useState('');
  const [status, setStatus] = useState('Active');
  const [roles, setRoles] = useState([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);

  useEffect(() => {
    api.get('/api/roles')
      .then((data) => {
        // The owner role cannot be given here (C-07).
        const list = (Array.isArray(data) ? data : []).filter((r) => r.name?.toLowerCase() !== 'owner');
        setRoles(list);
        if (list.length > 0) {
          const defaultRole = list.find((r) => r.name.toLowerCase() === 'biller') || list[0];
          setRoleId(defaultRole.id);
        }
      })
      .catch(() => {});
  }, []);

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!name.trim() || !email.trim()) {
      setError('Name and email are required.');
      return;
    }
    if (password && password.length < 12) {
      setError('A password needs at least 12 characters, or leave it empty to send an invitation.');
      return;
    }

    try {
      setSaving(true);
      setError(null);
      // Without a password the server emails an invitation link (C-05).
      await api.post('/api/users', {
        name: name.trim(),
        email: email.trim(),
        ...(password ? { password } : {}),
        role_id: roleId ? parseInt(roleId, 10) : undefined,
        status,
      }, { quiet: true });
      onSaved();
      onCancel();
    } catch (err) {
      setError(err?.message || 'Failed to create user');
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="space-y-4 p-2">
      <h3 className="text-lg font-bold text-gray-900 border-b pb-2">Add New User</h3>

      {error && <p className="text-sm text-red-600 bg-red-50 p-2 rounded">{error}</p>}

      <div>
        <label className="block text-sm font-medium text-gray-700 mb-1">
          Full Name <span className="text-red-500">*</span>
        </label>
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Enter user name"
          className="w-full px-3 py-2 border rounded-lg focus:ring-blue-500 focus:border-blue-500"
          required
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-gray-700 mb-1">
          Email Address <span className="text-red-500">*</span>
        </label>
        <input
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="Enter email address"
          className="w-full px-3 py-2 border rounded-lg focus:ring-blue-500 focus:border-blue-500"
          required
        />
      </div>

      <div>
        <label className="block text-sm font-medium text-gray-700 mb-1">
          Password
        </label>
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="Leave empty to email an invitation"
          autoComplete="new-password"
          className="w-full px-3 py-2 border rounded-lg focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="mt-1 text-xs text-gray-500">
          Empty: the user gets an email to set their own password. Otherwise at least 12 characters.
        </p>
      </div>

      <div>
        <label className="block text-sm font-medium text-gray-700 mb-1">
          Assigned Role <span className="text-red-500">*</span>
        </label>
        <select
          value={roleId}
          onChange={(e) => setRoleId(e.target.value)}
          className="w-full px-3 py-2 border rounded-lg focus:ring-blue-500 focus:border-blue-500 capitalize"
          required
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
          {saving ? 'Creating...' : 'Create User'}
        </button>
      </div>
    </form>
  );
}
