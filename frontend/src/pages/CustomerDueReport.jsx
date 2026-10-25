// frontend/src/pages/CustomerDueReport.jsx

import React, { useState, useEffect } from 'react';
import { NavLink } from 'react-router-dom';
import ListPageShell from '../components/layout/ListPageShell';
import CustomerDueReportList from '../components/lists/CustomerDueReportList';
import { api } from '../services/api'; // <-- IMPORT API

// --- REMOVED DUMMY DATA ---

export default function CustomerDueReport() {
  const [reports, setReports] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  // Fetch data from the API
  useEffect(() => {
    const fetchReports = async () => {
      try {
        setLoading(true);
        // The /api/invoices route has the due data we need
        const invoiceData = await api.get('/api/invoices');
        
        // Filter for only invoices that have an amount due
        const dueInvoices = invoiceData
          .filter(invoice => invoice.amountDue > 0)
          .map(invoice => ({ // Map to the structure expected by the list
            id: invoice.id,
            reference: invoice.invoiceNo,
            code: invoice.customer?.id ?? '-',
            customer: invoice.customer,
            totalAmount: invoice.amount,
            paid: invoice.paid,
            due: invoice.amountDue,
            status: invoice.status
          }));
        
        setReports(dueInvoices);
      } catch (err) {
        setError(err.message || 'Failed to fetch customer due reports');
        console.error(err);
      } finally {
        setLoading(false);
      }
    };
    fetchReports();
  }, []);



  return (
    <div className="flex h-full min-h-0 flex-1 flex-col">
      <div className="mb-4 shrink-0 border-b">
        <ul className="flex">
          <li className="mr-2">
            <NavLink
              to="/customer-report"
              end
              className={({ isActive }) =>
                `block px-6 py-3 text-sm font-medium ${
                  isActive
                    ? 'border-b-2 border-primary text-primary'
                    : 'text-muted-foreground hover:text-foreground'
                }`
              }
            >
              Customer Report
            </NavLink>
          </li>
          <li>
            <NavLink
              to="/customer-due-report"
              className={({ isActive }) =>
                `block px-6 py-3 text-sm font-medium ${
                  isActive
                    ? 'border-b-2 border-primary text-primary'
                    : 'text-muted-foreground hover:text-foreground'
                }`
              }
            >
              Customer Due
            </NavLink>
          </li>
        </ul>
      </div>

      <ListPageShell>
        <CustomerDueReportList reports={reports} loading={loading} error={error} />
      </ListPageShell>
    </div>
  );
}