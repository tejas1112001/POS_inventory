// frontend/src/pages/CustomerDueReport.jsx

import React, { useState, useEffect } from 'react';
import { NavLink, useLocation } from 'react-router-dom';
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
            code: invoice.customer.id, // Assuming customer ID is the code
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

  const location = useLocation();
  const isDue = location.pathname.endsWith('/due');

  const pageTitle = isDue ? 'Customer Due Report' : 'Customer Report';
  const pageDescription = isDue
    ? 'View outstanding dues for customers'
    : 'Manage your customer reports';

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col">
      <div className="mb-4 shrink-0 border-b">
        <ul className="flex">
          <li className="mr-2">
            <NavLink
              to="/customers/report"
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
              to="/customers/report/due"
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