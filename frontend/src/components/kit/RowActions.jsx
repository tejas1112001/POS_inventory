import { MoreHorizontal } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu';

// RowActions: one "…" menu per row. Actions the user may not take are
// left out (the server checks again).
export default function RowActions({ label, actions }) {
  const shown = actions.filter((a) => a && !a.hidden);
  if (!shown.length) return null;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button type="button" variant="ghost" size="icon" className="size-11 sm:size-8" aria-label={`Actions for ${label}`}>
          <MoreHorizontal className="size-4" aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-40">
        {shown.map((a) => (
          <DropdownMenuItem
            key={a.label}
            onSelect={a.onSelect}
            className={a.destructive ? 'text-danger focus:text-danger' : undefined}
          >
            {a.icon && <a.icon className="size-4" aria-hidden="true" />}
            {a.label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
