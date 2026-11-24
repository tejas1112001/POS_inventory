import { cn } from '@/lib/utils';

// TotalCard: the amount to pay on the brand gradient (payment sheets).
export default function TotalCard({ label = 'Total', value, large }) {
  return (
    <div className="rounded-xl bg-brand-gradient px-4 py-3 text-white shadow-sm">
      <p className="text-xs font-semibold text-white/85">{label}</p>
      <p className={cn('font-extrabold tabular-nums', large ? 'text-3xl' : 'text-2xl')}>{value}</p>
    </div>
  );
}
