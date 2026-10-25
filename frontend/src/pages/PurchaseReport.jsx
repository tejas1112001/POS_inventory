import ListPageShell from '../components/layout/ListPageShell';
import PurchaseReportList from '../components/lists/PurchaseReportList';

// The list loads the report itself, with the filters applied by the server.
export default function PurchaseReport() {
  return (
    <ListPageShell>
      <PurchaseReportList />
    </ListPageShell>
  );
}
