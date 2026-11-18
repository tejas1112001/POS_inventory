import { useEffect, useState } from 'react';
import { api } from '@/services/api';

// BatchSelect picks a batch of one variant (industry P3 item 4: the batch
// of a return without a sale). Empty means "not known": the units go to
// quarantine under the unknown batch.
export default function BatchSelect({ variantId, value, onChange, label = 'Batch', className }) {
  const [rows, setRows] = useState([]);

  useEffect(() => {
    if (!variantId) return undefined;
    let live = true;
    api
      .get(`/api/batches?variantId=${variantId}&includeEmpty=true`, { quiet: true })
      .then((r) => live && setRows((r?.items || []).filter((b) => !b.unknown)))
      .catch(() => live && setRows([]));
    return () => {
      live = false;
    };
  }, [variantId]);

  return (
    <select className={className} value={value ?? ''} aria-label={label} onChange={(e) => onChange(e.target.value)}>
      <option value="">Not known (quarantine)</option>
      {rows.map((b) => (
        <option key={b.id} value={b.id}>
          {b.batchNo}
          {b.expiry ? ` · Exp ${b.expiry}` : ''}
          {b.expired ? ' (expired)' : ''}
        </option>
      ))}
    </select>
  );
}
