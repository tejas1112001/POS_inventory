import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { Menu, ChevronDown, LogOut, KeyRound, Settings, Search, Moon, Sun, ShoppingCart, CornerDownLeft } from 'lucide-react';
import ChangePasswordDialog from '@/components/ChangePasswordDialog';
import { Link, useNavigate } from 'react-router-dom';
import { usePos } from '@/hooks/usePos';
import { usePermission } from '@/hooks/usePermission';
import { useModules } from '@/hooks/useModules';
import { useTheme } from '@/context/ThemeContext';
import ThemeToggle from '@/components/kit/ThemeToggle';
import IconTile from '@/components/kit/IconTile';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import { initials } from '@/lib/format';
import { cn } from '@/lib/utils';
import { visibleNav } from './navigation';

// PageJump: the search box in the top bar. It finds a page of the menu by
// name (Ctrl/⌘+K focuses it); Enter or a click opens it.
function PageJump() {
  const { hasPermission } = usePermission();
  const { hasModule } = useModules();
  const navigate = useNavigate();
  const [q, setQ] = useState('');
  const [open, setOpen] = useState(false);
  const [hi, setHi] = useState(0);
  const input = useRef(null);
  const listId = useId();

  const items = useMemo(
    () => visibleNav(hasPermission, hasModule).flatMap((s) => s.items.map((i) => ({ ...i, section: s.title }))),
    [hasPermission, hasModule]
  );
  const term = q.trim().toLowerCase();
  const matches = term ? items.filter((i) => i.label.toLowerCase().includes(term) || i.section.toLowerCase().includes(term)).slice(0, 7) : [];

  useEffect(() => {
    const onKey = (e) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        input.current?.focus();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const go = (item) => {
    if (!item) return;
    navigate(item.path);
    setQ('');
    setOpen(false);
    input.current?.blur();
  };

  const onKeyDown = (e) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setHi((h) => Math.min(h + 1, matches.length - 1));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setHi((h) => Math.max(h - 1, 0));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      go(matches[hi]);
    } else if (e.key === 'Escape') {
      setQ('');
      setOpen(false);
      input.current?.blur();
    }
  };

  const showList = open && term.length > 0;

  return (
    <div className="relative hidden w-full max-w-sm md:block">
      <Search className="pointer-events-none absolute top-1/2 left-3.5 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
      <input
        ref={input}
        type="text"
        role="combobox"
        aria-expanded={showList}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-label="Go to a page"
        placeholder="Search pages…"
        value={q}
        onChange={(e) => {
          setQ(e.target.value);
          setHi(0);
          setOpen(true);
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setTimeout(() => setOpen(false), 120)}
        onKeyDown={onKeyDown}
        className="h-10 w-full rounded-full border border-border bg-card pr-14 pl-10 text-sm shadow-xs transition-[border-color,box-shadow] outline-none placeholder:text-muted-foreground focus-visible:border-primary focus-visible:ring-[3px] focus-visible:ring-ring/25"
      />
      <kbd className="pointer-events-none absolute top-1/2 right-3 hidden -translate-y-1/2 rounded-md border bg-muted px-1.5 py-0.5 text-[10px] font-semibold text-muted-foreground lg:block">
        Ctrl K
      </kbd>
      {showList && (
        <ul id={listId} role="listbox" aria-label="Pages" className="absolute top-12 right-0 left-0 z-50 overflow-hidden rounded-2xl border bg-popover p-1.5 shadow-lg">
          {matches.length === 0 ? (
            <li className="px-3 py-2.5 text-sm text-muted-foreground">No page called “{q.trim()}”.</li>
          ) : (
            matches.map((m, i) => {
              return (
                <li key={m.path} role="option" aria-selected={i === hi}>
                  <button
                    type="button"
                    onMouseDown={(e) => e.preventDefault()}
                    onMouseEnter={() => setHi(i)}
                    onClick={() => go(m)}
                    className={cn('flex w-full items-center gap-3 rounded-xl px-3 py-2 text-left text-sm', i === hi ? 'bg-accent text-accent-foreground' : 'text-foreground')}
                  >
                    <IconTile icon={m.icon} size="sm" />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate font-medium">{m.label}</span>
                      <span className="block truncate text-xs text-muted-foreground">{m.section}</span>
                    </span>
                    {i === hi && <CornerDownLeft className="size-3.5 text-muted-foreground" aria-hidden="true" />}
                  </button>
                </li>
              );
            })
          )}
        </ul>
      )}
    </div>
  );
}

export default function Topbar({ title, onOpenMobileMenu }) {
  const { currentUser, logout } = usePos();
  const { hasPermission } = usePermission();
  const { mode, toggleMode } = useTheme();
  const navigate = useNavigate();
  const [pwOpen, setPwOpen] = useState(false);
  const dark = mode === 'dark';

  const handleSignOut = async () => {
    await logout();
    navigate('/login', { replace: true });
  };

  return (
    <header className="z-20 flex h-16 shrink-0 items-center gap-3 border-b border-border/70 bg-card/85 px-3 sm:px-4 lg:px-5">
      <Button variant="ghost" size="icon" className="size-10 lg:hidden" onClick={onOpenMobileMenu} aria-label="Open menu">
        <Menu className="size-5" />
      </Button>

      <h1 className="min-w-0 shrink-0 truncate text-lg font-bold tracking-tight sm:text-xl">{title}</h1>

      <div className="ml-auto flex min-w-0 flex-1 items-center justify-end gap-2 sm:gap-2.5">
        <PageJump />

        {hasPermission('pos', 'view') && title !== 'Checkout' && (
          <Button asChild className="hidden h-10 rounded-full px-4 xl:inline-flex">
            <Link to="/pos">
              <ShoppingCart className="size-4" aria-hidden="true" /> New sale
            </Link>
          </Button>
        )}

        <ThemeToggle />

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" className="flex h-10 items-center gap-2 rounded-full pr-2.5 pl-1">
              <Avatar className="size-8">
                <AvatarFallback className="bg-brand-gradient text-xs font-bold text-white">
                  {currentUser ? initials(currentUser.name) : 'U'}
                </AvatarFallback>
              </Avatar>
              <span className="hidden max-w-[140px] truncate text-sm font-semibold text-navy sm:inline">{currentUser?.name}</span>
              <ChevronDown className="size-4 text-muted-foreground" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-60">
            <DropdownMenuLabel>
              <div className="flex flex-col">
                <span className="text-sm font-semibold">{currentUser?.name}</span>
                <span className="truncate text-xs font-normal text-muted-foreground">{currentUser?.email}</span>
              </div>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => navigate('/settings')}>
              <Settings className="size-4" /> Settings
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => setPwOpen(true)}>
              <KeyRound className="size-4" /> Change password
            </DropdownMenuItem>
            <DropdownMenuItem onClick={toggleMode}>
              {dark ? <Sun className="size-4" /> : <Moon className="size-4" />} {dark ? 'Light mode' : 'Dark mode'}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem className="text-destructive focus:text-destructive" onClick={handleSignOut}>
              <LogOut className="size-4" /> Sign out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <ChangePasswordDialog open={pwOpen} onClose={() => setPwOpen(false)} />
    </header>
  );
}
