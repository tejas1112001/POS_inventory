import RecordsPage from '@/components/kit/RecordsPage';
import { categoryConfig } from './recordConfigs';

// Categories: server-paged list with search, status filter and add/edit in a sheet.
export default function Categories({ startNew = false }) {
  return <RecordsPage config={categoryConfig} startNew={startNew} />;
}
