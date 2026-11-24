import IconTile from './IconTile';
import Delta from './Delta';

// StatCard: a headline figure (moodboard KPI card): icon tile, label, value
// and the change against the previous period.
export default function StatCard({ icon, tone = 'teal', label, value, delta, hint }) {
  return (
    <div className="flex h-full items-center gap-4 rounded-2xl border bg-card bg-card-gradient p-4 shadow-xs sm:p-5">
      <IconTile icon={icon} tone={tone} size="lg" />
      <div className="min-w-0 flex-1 space-y-1">
        <p className="truncate text-xs font-medium text-muted-foreground sm:text-sm">{label}</p>
        <p className="truncate text-xl font-extrabold tracking-tight text-navy tabular-nums sm:text-2xl">{value}</p>
        <Delta delta={delta} hint={hint} />
      </div>
    </div>
  );
}
