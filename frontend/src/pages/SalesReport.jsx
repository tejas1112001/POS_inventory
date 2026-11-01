import ReportTabs from '../components/layout/ReportTabs';
import ListPageShell from '../components/layout/ListPageShell';
import SalesReportList from '../components/lists/SalesReportList';

// The list loads the report itself, with the filters applied by the server.
export default function SalesReport() {
  return (
    <ListPageShell>
      <ReportTabs />
      <SalesReportList />
    </ListPageShell>
  );
}
