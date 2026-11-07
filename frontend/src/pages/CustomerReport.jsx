// frontend/src/pages/CustomerReport.jsx

import ReportTabs from '../components/layout/ReportTabs';
import React, { useState, useEffect } from 'react';
import { NavLink } from 'react-router-dom';
import ListPageShell from '../components/layout/ListPageShell';
import CustomerReportList from '../components/lists/CustomerReportList';
import { api } from '../services/api'; // <-- IMPORT API

// --- REMOVED DUMMY DATA ---

export default function CustomerReport() {
  const [reports, setReports] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  // Fetch data from the API
  useEffect(() => {
    const fetchReports = async () => {
      try {
        setLoading(true);
        // This endpoint returns sales data, which is what this report shows
        const data = await api.get('/api/sales/report');
        
        // One row per customer, from the sold lines (returns netted).
        const byCustomer = {};
        (Array.isArray(data) ? data : []).forEach((item) => {
          const name = item.customerName || item.customer?.name || 'Walk-in Customer';
          const key = item.customer?.id ?? name;
          const c = (byCustomer[key] ||= {
            id: String(key),
            reference: item.reference,
            customer: { name },
            sales: new Set(),
            items: 0,
            amount: 0,
            reportDate: item.date,
            paymentMethod: '-',
            status: 'Completed',
          });
          c.sales.add(item.saleId);
          c.items += Number(item.soldQty) || 0;
          c.amount += Number(item.netAmount ?? item.soldAmount) || 0;
          if (new Date(item.date) > new Date(c.reportDate)) {
            c.reportDate = item.date;
            c.reference = item.reference;
          }
        });
        const customerAggregates = Object.fromEntries(
          Object.entries(byCustomer).map(([k, c]) => [k, { ...c, totalOrders: c.sales.size, sales: undefined }])
        );

        setReports(Object.values(customerAggregates));
      } catch (err) {
        setError(err.message || 'Failed to fetch customer reports');
        console.error(err);
      } finally {
        setLoading(false);
      }
    };
    fetchReports();
  }, []);


  
  return (
    <div className="flex h-full min-h-0 flex-1 flex-col">
      <ReportTabs />

      <ListPageShell>
        <CustomerReportList reports={reports} loading={loading} error={error} />
      </ListPageShell>
    </div>
  );
}