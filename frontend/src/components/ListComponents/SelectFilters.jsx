import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';

export default function SelectFilters({
  statusFilter,
  statusOptions = [],
  setStatusFilter,
  placeholder = 'Status',
  compact = true,
}) {
  return (
    <div className="flex min-w-[140px] flex-col sm:min-w-[160px]">
      <Label className={compact ? 'sr-only' : 'mb-1.5'}>{placeholder}</Label>
      <Select value={statusFilter} onValueChange={setStatusFilter}>
        <SelectTrigger className="h-9 w-full">
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          {statusOptions.map((status) => (
            <SelectItem key={status} value={status}>
              {status}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
