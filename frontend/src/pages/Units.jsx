import RecordsPage from '@/components/kit/RecordsPage';
import { unitConfig } from './recordConfigs';

// Units: server-paged list with search, status filter and add/edit in a sheet.
export default function Units({ startNew = false }) {
  return <RecordsPage config={unitConfig} startNew={startNew} />;
}
