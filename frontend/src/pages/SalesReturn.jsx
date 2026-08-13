import React, { useState, useEffect } from 'react';
import ListPageShell from '../components/layout/ListPageShell';
import SalesReturnList from '../components/lists/SalesReturnList';
import { api } from '../services/api';

export default function SalesReturn() {
  const [salesReturns, setSalesReturns] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [showForm, setShowForm] = useState(false);

  useEffect(() => {
    fetchSalesReturns();
  }, []);

  const fetchSalesReturns = async () => {
    try {
      setLoading(true);
      const data = await api.get('/api/sales/returns');
      setSalesReturns(data);
    } catch (err) {
      setError('Failed to fetch sales returns');
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  if (loading) return <div className="p-6">Loading sales returns...</div>;
  if (error) return <div className="p-6 text-red-500">{error}</div>;

  return (
    <ListPageShell>
      <SalesReturnList
        salesReturns={salesReturns}
        setShowForm={setShowForm}
      />
    </ListPageShell>
  );
}