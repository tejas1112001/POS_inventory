import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Label } from '@/components/ui/label';
import { cn } from '@/lib/utils';

export default function SelectField({
  name,
  label,
  value,
  onChange,
  options = [],
  placeholder = null,
  colClass = 'md:col-span-3',
  className = '',
  required = false,
  compact = true,
  renderOption = null,
}) {
  const normalized = options.map((opt) =>
    typeof opt === 'string' ? { value: opt, label: opt } : opt
  );

  const selectControl = (
    <Select value={value} onValueChange={onChange} required={required}>
      <SelectTrigger
        id={name}
        className={cn('h-9 w-full', compact ? '' : className)}
        aria-label={label || name}
      >
        <SelectValue placeholder={placeholder || label} />
      </SelectTrigger>
      <SelectContent>
        {normalized.map((opt, i) => (
          <SelectItem key={i} value={opt.value}>
            {renderOption ? renderOption(opt) : opt.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );

  if (compact) {
    return (
      <div className="flex w-full min-w-[160px] flex-col sm:w-auto sm:min-w-[180px]">
        <Label htmlFor={name} className="sr-only">
          {label || name}
        </Label>
        {selectControl}
      </div>
    );
  }

  return (
    <div className={colClass}>
      {label && (
        <Label htmlFor={name} className="mb-1.5">
          {label}
        </Label>
      )}
      {selectControl}
    </div>
  );
}
