import React, { useState } from 'react';
import { Palette, Check } from 'lucide-react';
import ShopSettings from '@/components/settings/ShopSettings';
import EmailSettings from '@/components/settings/EmailSettings';
import { usePermission } from '@/context/PermissionContext';
import { SectionCard } from '@/components/shared/section-card';
import { useTheme, THEME_PRESETS } from '@/context/ThemeContext';
import { cn } from '@/lib/utils';

// Settings: the shop's profile, tax and receipts (settings:view/edit), its
// SMTP server (email_settings:view/edit) and this browser's theme.
export default function Settings() {
  const { hasPermission } = usePermission();
  const tabs = [
    hasPermission('settings', 'view') && { key: 'shop', label: 'Shop' },
    hasPermission('email_settings', 'view') && { key: 'email', label: 'Email' },
    { key: 'appearance', label: 'Appearance' },
  ].filter(Boolean);
  const [tab, setTab] = useState(tabs[0].key);

  return (
    <div className="flex h-full flex-col gap-3 p-3 sm:p-4">
      <div role="tablist" className="flex gap-1 border-b">
        {tabs.map((t) => (
          <button
            key={t.key}
            role="tab"
            type="button"
            aria-selected={tab === t.key}
            onClick={() => setTab(t.key)}
            className={cn(
              '-mb-px border-b-2 px-4 py-2 text-sm font-medium',
              tab === t.key ? 'border-primary text-primary' : 'border-transparent text-muted-foreground hover:text-foreground'
            )}
          >
            {t.label}
          </button>
        ))}
      </div>
      {tab === 'shop' && <ShopSettings canEdit={hasPermission('settings', 'edit')} />}
      {tab === 'email' && <EmailSettings canEdit={hasPermission('email_settings', 'edit')} />}
      {tab === 'appearance' && <Appearance />}
    </div>
  );
}

function Appearance() {
  const { theme, setTheme } = useTheme();
  return (
    <SectionCard title="Theme Color">
      <p className="mb-4 text-sm text-muted-foreground">
        Choose an accent color for buttons, sidebar highlights and form actions. It applies to this browser only.
      </p>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        {Object.entries(THEME_PRESETS).map(([key, preset]) => (
          <button
            key={key}
            type="button"
            onClick={() => setTheme(key)}
            className={cn(
              'group relative flex flex-col items-center gap-2 rounded-xl border-2 p-4 transition-all hover:shadow-md',
              theme === key
                ? 'border-primary bg-primary/5 shadow-sm'
                : 'border-border bg-card hover:border-primary/40'
            )}
          >
            <div
              className="flex size-10 items-center justify-center rounded-full shadow-sm"
              style={{ background: preset.primary }}
            >
              <Palette className="size-5 text-white" />
            </div>
            <span className="text-sm font-medium">{preset.label}</span>
            {theme === key && (
              <span className="absolute right-2 top-2 flex size-5 items-center justify-center rounded-full bg-primary text-primary-foreground">
                <Check className="size-3" />
              </span>
            )}
          </button>
        ))}
      </div>
    </SectionCard>
  );
}
