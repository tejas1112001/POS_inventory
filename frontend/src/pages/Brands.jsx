import RecordsPage from '@/components/kit/RecordsPage';
import { brandConfig } from './recordConfigs';

// Brands: server-paged list with search, status filter and add/edit in a sheet.
export default function Brands({ startNew = false }) {
  return <RecordsPage config={brandConfig} startNew={startNew} />;
}
