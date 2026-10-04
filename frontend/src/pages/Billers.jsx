import React, { useState, useEffect } from 'react';
import ListPageShell from '../components/layout/ListPageShell';
import BillerList from '../components/lists/BillerList';
import { api } from '../services/api';
import { usePermission } from '../hooks/usePermission';

export default function Billers() {
  const [billers, setBillers] = useState([]);
  const [showForm, setShowForm] = useState(false);
  const { hasPermission } = usePermission();

  useEffect(() => {
    fetchBillers();
  }, []);

  const fetchBillers = async () => {
    try {
      const billersData = await api.get('/api/billers');
      setBillers(billersData || []);
    } catch (error) {
      console.error('Failed to fetch users:', error);
    }
  };

  return (
    <ListPageShell>
      <BillerList
        billers={billers}
        showForm={showForm}
        setShowForm={setShowForm}
        onRefresh={fetchBillers}
      />
    </ListPageShell>
  );
}
