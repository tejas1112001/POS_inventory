import { Search } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

export default function SearchInput({ search, setSearch, placeholder = 'Search...', compact = true }) {
  return (
    <div className="flex w-full min-w-[160px] flex-col sm:w-auto sm:min-w-[180px]">
      <Label className={compact ? 'sr-only' : 'mb-1.5'}>Search</Label>
      <div className="relative w-full">
        <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          type="text"
          className="h-9 pl-9"
          placeholder={placeholder}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </div>
    </div>
  );
}
