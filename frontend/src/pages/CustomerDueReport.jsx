import { useEffect, useState } from 'react';
import ReportTabs from '../components/layout/ReportTabs';
import ListPageShell from '../components/layout/ListPageShell';
import CustomerDueReportList from '../components/lists/CustomerDueReportList';
import { api } from '../services/api';

// Sales with money still due, from the invoices list.
export default function CustomerDueReport() {
  const [reports, setReports] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let live = true;
    api
      .get('/api/invoices', { quiet: true })
      .then((data) => {
        if (!live) return;
        setReports(
          (Array.isArray(data) ? data : [])
            .filter((invoice) => invoice.amountDue > 0)
            .map((invoice) => ({
              id: invoice.id,
              reference: invoice.invoiceNo,
              customer: invoice.customer,
              dueDate: invoice.dueDate,
              totalAmount: invoice.amount,
              paid: invoice.paid,
              due: invoice.amountDue,
              status: invoice.status,
            }))
        );
      })
      .catch((err) => live && setError(err.message || 'The dues could not be loaded.'))
      .finally(() => live && setLoading(false));
    return () => {
      live = false;
    };
  }, []);

  return (
    <ListPageShell scroll>
      <ReportTabs />
      <CustomerDueReportList reports={reports} loading={loading} error={error} />
    </ListPageShell>
  );
}
