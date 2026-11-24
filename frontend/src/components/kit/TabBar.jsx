import { Link, NavLink } from 'react-router-dom';
import { cn } from '@/lib/utils';

const tabClass = (active) =>
  cn(
    '-mb-px inline-flex min-h-11 shrink-0 items-center whitespace-nowrap border-b-2 px-3 text-sm font-semibold transition-colors sm:px-4',
    active ? 'border-primary text-primary' : 'border-transparent text-muted-foreground hover:text-foreground'
  );

// TabBar: one row of tabs under the top bar. Tabs: [{ key, label, to?, plain? }].
// A tab with `to` is a link (NavLink, active by route; plain: never
// shown active, for a tab that leaves to another page);
// the others switch value/onChange in place (role="tab"). The row never
// shrinks and scrolls sideways on narrow screens; it bleeds to the screen
// edge on phones so the last tab is not cut by the page padding.
export default function TabBar({ tabs, value, onChange, label, className }) {
  const inPage = tabs.some((t) => !t.to);
  return (
    <nav aria-label={label} className={cn('-mx-3 shrink-0 overflow-x-auto border-b px-3 no-scrollbar sm:mx-0 sm:px-0', className)}>
      <div role={inPage ? 'tablist' : undefined} className="flex min-w-max gap-1">
        {tabs.map((t) => {
          if (t.to && t.plain) {
            return (
              <Link key={t.key || t.to} to={t.to} className={tabClass(false)}>
                {t.label}
              </Link>
            );
          }
          if (t.to) {
            return (
              <NavLink key={t.key || t.to} to={t.to} className={({ isActive }) => tabClass(isActive)}>
                {t.label}
              </NavLink>
            );
          }
          return (
            <button key={t.key} type="button" role="tab" aria-selected={value === t.key} onClick={() => onChange(t.key)} className={tabClass(value === t.key)}>
              {t.label}
            </button>
          );
        })}
      </div>
    </nav>
  );
}
