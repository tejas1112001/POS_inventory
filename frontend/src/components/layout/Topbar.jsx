import { Menu, ChevronDown, LogOut, User as UserIcon, Settings } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { usePos } from '@/hooks/usePos';
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
import { getRouteTitle } from './routeTitles';

export default function Topbar({ title, onOpenMobileMenu }) {
  const { currentUser, logout } = usePos();
  const navigate = useNavigate();

  const handleSignOut = () => {
    logout();
    navigate('/signin');
  };

  return (
    <header className="z-20 flex h-16 shrink-0 items-center gap-2 border-b bg-background px-3 sm:px-4">
      <Button
        variant="ghost"
        size="icon"
        className="lg:hidden"
        onClick={onOpenMobileMenu}
        aria-label="Open menu"
      >
        <Menu className="size-5" />
      </Button>

      <h1 className="text-base font-semibold tracking-tight">{title}</h1>

      <div className="ml-auto flex items-center gap-2">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" className="flex h-9 items-center gap-2 px-2">
              <Avatar className="size-7">
                <AvatarFallback className="bg-primary/10 text-primary text-xs font-semibold">
                  {currentUser ? initials(currentUser.name) : 'U'}
                </AvatarFallback>
              </Avatar>
              <span className="hidden text-sm font-medium sm:inline">{currentUser?.name}</span>
              <ChevronDown className="size-4 text-muted-foreground" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuLabel>
              <div className="flex flex-col">
                <span className="text-sm font-medium">{currentUser?.name}</span>
                <span className="text-xs text-muted-foreground">{currentUser?.email}</span>
              </div>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => navigate('/settings')}>
              <Settings className="size-4" /> Settings
            </DropdownMenuItem>
            <DropdownMenuItem>
              <UserIcon className="size-4" /> Profile
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem className="text-destructive focus:text-destructive" onClick={handleSignOut}>
              <LogOut className="size-4" /> Sign Out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  );
}

export function usePageTitle(pathname) {
  return getRouteTitle(pathname);
}
