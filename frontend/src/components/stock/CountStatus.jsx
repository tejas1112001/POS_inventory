import { Badge } from '@/components/ui/badge';

// CountStatus is a stock count's status (Draft, Completed, Cancelled).
export default function CountStatus({ status }) {
  const variant = status === 'Draft' ? 'secondary' : status === 'Completed' ? 'default' : 'outline';
  return <Badge variant={variant}>{status}</Badge>;
}
