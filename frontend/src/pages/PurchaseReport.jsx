import React, { useState, useEffect } from 'react';
import ListPageShell from '../components/layout/ListPageShell';
import PurchaseReportList from '../components/lists/PurchaseReportList';
import { api } from '../services/api';

export default function PurchaseReport() {
  const [reports, setReports] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    fetchPurchaseReports();
  }, []);

  const fetchPurchaseReports = async () => {
    try {
      setLoading(true);
      const data = await api.get('/api/purchase/report');
      setReports(data);
    } catch (err) {
      setError('Failed to fetch purchase reports');
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  if (loading) return <div className="p-6">Loading purchase reports...</div>;
  if (error) return <div className="p-6 text-red-500">{error}</div>;

  return (
    <ListPageShell>
      <PurchaseReportList reports={reports} />
    </ListPageShell>
  );
}