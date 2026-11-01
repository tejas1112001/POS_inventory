import { useState } from 'react';
import { useConfirm } from '@/components/feedback/ConfirmDialog';
import { usePermission } from '@/context/PermissionContext';
import { api } from '@/services/api';
import { toast } from '../../../shared/toast/store';
import PurchasePaymentSheet from './PurchasePaymentSheet';

const n = (v) => Number(v) || 0;
const open = (p) => p && (p.status === 'Ordered' || p.status === 'Pending');

// usePurchaseActions: receive (purchases:approve), cancel (purchases:edit)
// and record payment (purchases:edit), with their confirm windows.
export default function usePurchaseActions(onChanged) {
  const confirm = useConfirm();
  const { hasPermission } = usePermission();
  const [paying, setPaying] = useState(null);

  const units = (p) => (p.items || []).reduce((s, i) => s + n(i.quantity), 0);
  const stores = (p) => [...new Set((p.items || []).map((i) => i.store?.name).filter(Boolean))].join(', ');

  const receive = async (p) => {
    const ok = await confirm({
      title: `Receive ${p.reference}?`,
      body: `${units(p)} units go into stock${stores(p) ? ` at ${stores(p)}` : ''}. This is done once and cannot be undone; later corrections are stock adjustments.`,
      confirmLabel: 'Receive into stock',
    });
    if (!ok) return;
    try {
      const out = await api.put(`/api/purchases/${p.id}`, { status: 'Received' }, { quiet: true });
      toast.success(`${p.reference} received: ${units(p)} units added to stock.`);
      onChanged?.(out);
    } catch (err) {
      toast.error(err.message || 'The purchase could not be received.');
    }
  };

  const cancel = async (p) => {
    const ok = await confirm({
      title: `Cancel ${p.reference}?`,
      body: 'The purchase stays in the list as Cancelled and cannot be reopened. No stock changes.',
      confirmLabel: 'Cancel purchase',
      cancelLabel: 'Keep it',
      destructive: true,
    });
    if (!ok) return;
    try {
      const out = await api.put(`/api/purchases/${p.id}`, { status: 'Cancelled' }, { quiet: true });
      toast.success(`${p.reference} cancelled.`);
      onChanged?.(out);
    } catch (err) {
      toast.error(err.message || 'The purchase could not be cancelled.');
    }
  };

  return {
    canReceive: (p) => open(p) && hasPermission('purchases', 'approve'),
    canCancel: (p) => open(p) && hasPermission('purchases', 'edit'),
    canPay: (p) => p && p.status !== 'Cancelled' && n(p.due) > 0 && hasPermission('purchases', 'edit'),
    receive,
    cancel,
    pay: setPaying,
    sheet: <PurchasePaymentSheet purchase={paying} onClose={() => setPaying(null)} onDone={onChanged} />,
  };
}
