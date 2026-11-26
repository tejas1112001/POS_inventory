import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { cn } from '@/lib/utils';

// ReportSelect: a labelled choice on a report's filter line (GST returns,
// expiry), in the app's own dropdown instead of the browser's.
//   options: [{ value, label }]; empty: the text shown when there is no
//   option (the control is then disabled).
export default function ReportSelect({ id, label, value, onChange, options, empty, className }) {
  const none = options.length === 0;
  return (
    <div className={cn('flex min-w-0 items-center gap-2 max-sm:w-full', className)}>
      <label htmlFor={id} className="shrink-0 text-[13px] font-semibold text-muted-foreground">
        {label}
      </label>
      <Select value={none ? undefined : value} onValueChange={onChange} disabled={none}>
        <SelectTrigger id={id} className="h-10 min-w-[10rem] text-[13px] max-sm:flex-1 sm:h-9">
          <SelectValue placeholder={empty || 'None'} />
        </SelectTrigger>
        <SelectContent>
          {options.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
