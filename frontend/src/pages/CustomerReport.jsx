import ReportTabs from '../components/layout/ReportTabs';
import ListPageShell from '../components/layout/ListPageShell';
import CustomerReportList from '../components/lists/CustomerReportList';

// Sales per customer; the list loads the sales report for its dates.
export default function CustomerReport() {
  return (
    <ListPageShell scroll>
      <ReportTabs />
      <CustomerReportList />
    </ListPageShell>
  );
}
