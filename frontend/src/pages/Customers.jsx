import RecordsPage from '@/components/kit/RecordsPage';
import { customerConfig } from './recordConfigs';

// Customers: server-paged list with search, status filter and add/edit in a sheet.
export default function Customers({ startNew = false }) {
  return <RecordsPage config={customerConfig} startNew={startNew} />;
}
