import RecordsPage from '@/components/kit/RecordsPage';
import { supplierConfig } from './recordConfigs';

// Suppliers: server-paged list with search, status filter and add/edit in a sheet.
export default function Suppliers({ startNew = false }) {
  return <RecordsPage config={supplierConfig} startNew={startNew} />;
}
