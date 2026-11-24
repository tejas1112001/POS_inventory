import { Moon, Sun } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { useAppDispatch, useAppSelector } from '@/store/hooks';
import { selectMode, toggleMode } from '@/store/uiSlice';

// ThemeToggle: the light / dark switch (state in the Redux store).
export default function ThemeToggle({ className = 'size-10 rounded-full' }) {
  const dispatch = useAppDispatch();
  const dark = useAppSelector(selectMode) === 'dark';
  return (
    <Button
      variant="outline"
      size="icon"
      className={className}
      onClick={() => dispatch(toggleMode())}
      aria-label={dark ? 'Switch to light mode' : 'Switch to dark mode'}
      title={dark ? 'Light mode' : 'Dark mode'}
    >
      {dark ? <Sun className="size-[18px]" /> : <Moon className="size-[18px]" />}
    </Button>
  );
}
