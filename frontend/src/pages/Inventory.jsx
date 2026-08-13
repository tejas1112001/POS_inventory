import React, { useState, useEffect } from 'react';
import { api } from '../services/api';
import ListPageShell from '../components/layout/ListPageShell';
import InventoryList from '../components/lists/InventoryList';

export default function Inventory() {
  const [inventory, setInventory] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const fetchInventory = async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await api.get('/api/inventory');
      setInventory(Array.isArray(data) ? data : []);
    } catch (err) {
      setError('Failed to load inventory');
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchInventory();
  }, []);

  if (loading) return <div className="p-6 text-gray-500">Loading inventory…</div>;
  if (error) return <div className="p-6 text-red-500">{error}</div>;

  return (
    <ListPageShell>
      <InventoryList inventory={inventory} onRefresh={fetchInventory} />
    </ListPageShell>
  );
}
