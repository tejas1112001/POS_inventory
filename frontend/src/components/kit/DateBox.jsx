// DateBox: one date filter (YYYY-MM-DD), for FilterBar's extra slot.
export default function DateBox({ label, value, onChange, min, max }) {
  return (
    <label className="flex items-center gap-1.5 text-sm text-muted-foreground">
      <span className="sm:sr-only">{label}</span>
      <input type="date" aria-label={label} value={value || ''} min={min} max={max} onChange={(e) => onChange(e.target.value)} className="h-11 rounded-md border bg-background px-2 text-sm text-foreground sm:h-9" />
    </label>
  );
}
