import React, { useState, useEffect } from 'react';
import ListPageShell from '../components/layout/ListPageShell';
import SalesReportList from '../components/lists/SalesReportList';
import { api } from '../services/api';

export default function SalesReport() {
  const [reports, setReports] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    fetchSalesReports();
  }, []);

  const fetchSalesReports = async () => {
    try {
      setLoading(true);
      const data = await api.get('/api/sales/report');
      setReports(data);
    } catch (err) {
      setError('Failed to fetch sales reports');
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  if (loading) return <div className="p-6">Loading sales reports...</div>;
  if (error) return <div className="p-6 text-red-500">{error}</div>;

  return (
    <ListPageShell>
      <SalesReportList reports={reports} />
    </ListPageShell>
  );
}