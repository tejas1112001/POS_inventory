import { Link } from 'react-router-dom';
import React, { useState } from 'react';
import { Palette, Check, Moon, Sun } from 'lucide-react';
import ShopSettings from '@/components/settings/ShopSettings';
import EmailSettings from '@/components/settings/EmailSettings';
import BusinessSettings from '@/components/settings/BusinessSettings';
import ScannerSettings from '@/components/settings/ScannerSettings';
import ScaleSettings from '@/components/settings/ScaleSettings';
import LabelSettings from '@/components/settings/LabelSettings';
import ReturnPolicySettings from '@/components/settings/ReturnPolicySettings';
import BatchSettings from '@/components/settings/BatchSettings';
import { useModules } from '@/hooks/useModules';
import { usePermission } from '@/context/PermissionContext';
import { SectionCard } from '@/components/shared/section-card';
import { useTheme, THEME_PRESETS } from '@/context/ThemeContext';
import { cn } from '@/lib/utils';
import ChoiceButton from '@/components/kit/ChoiceButton';

// Settings: the shop's profile, tax and receipts (settings:view/edit), its
// SMTP server (email_settings:view/edit) and this browser's theme.
export default function Settings() {
  const { hasPermission } = usePermission();
  const { hasModule } = useModules();
  const tabs = [
    hasPermission('settings', 'view') && { key: 'shop', label: 'Shop' },
    hasPermission('email_settings', 'view') && { key: 'email', label: 'Email' },
    hasPermission('settings', 'view') && hasModule('barcode_labels') && { key: 'labels', label: 'Labels' },
    hasPermission('settings', 'view') && { key: 'returns', label: 'Returns' },
    hasPermission('settings', 'view') && hasModule('batch_expiry') && { key: 'batches', label: 'Batches' },
    { key: 'appearance', label: 'Appearance' },
    hasPermission('stores', 'view') && { key: 'stores', label: 'Stores', to: '/stores' },
    hasPermission('billing', 'view') && { key: 'billing', label: 'Plan & billing', to: '/billing' },
  ].filter(Boolean);
  const [tab, setTab] = useState(tabs[0].key);

  return (
    <div className="flex h-full flex-col gap-3">
      <div role="tablist" className="-mx-3 flex gap-1 overflow-x-auto border-b px-3 sm:mx-0 sm:px-0">
        {tabs.map((t) => t.to ? (
          <Link key={t.key} to={t.to} className="-mb-px inline-flex min-h-11 items-center whitespace-nowrap border-b-2 border-transparent px-4 text-sm font-semibold text-muted-foreground hover:text-foreground">
            {t.label}
          </Link>
        ) : (
          <button
            key={t.key}
            role="tab"
            type="button"
            aria-selected={tab === t.key}
            onClick={() => setTab(t.key)}
            className={cn(
              '-mb-px min-h-11 whitespace-nowrap border-b-2 px-4 text-sm font-semibold',
              tab === t.key ? 'border-primary text-primary' : 'border-transparent text-muted-foreground hover:text-foreground'
            )}
          >
            {t.label}
          </button>
        ))}
      </div>
      {tab === 'shop' && (
        <div className="space-y-3">
          <BusinessSettings canManage={hasPermission('modules', 'manage')} />
          <ShopSettings canEdit={hasPermission('settings', 'edit')} />
        </div>
      )}
      {tab === 'email' && <EmailSettings canEdit={hasPermission('email_settings', 'edit')} />}
      {tab === 'labels' && <LabelSettings canEdit={hasPermission('settings', 'edit')} canPrint={hasPermission('labels', 'print')} />}
      {tab === 'returns' && <ReturnPolicySettings canEdit={hasPermission('settings', 'edit')} />}
      {tab === 'batches' && <BatchSettings canEdit={hasPermission('settings', 'edit')} />}
      {tab === 'appearance' && (
        <div className="space-y-3">
          <Appearance />
          <ScannerSettings />
          <ScaleSettings canEdit={hasPermission('settings', 'edit')} />
        </div>
      )}
    </div>
  );
}

function Appearance() {
  const { theme, setTheme, mode, setMode } = useTheme();
  return (
    <SectionCard title="Appearance">
      <p className="mb-3 text-sm text-muted-foreground">Light or dark screens, and the accent colour for buttons and highlights. Both apply to this browser only.</p>
      <div className="mb-5 grid max-w-sm grid-cols-2 gap-2" role="radiogroup" aria-label="Mode">
        <ChoiceButton selected={mode === 'light'} icon={Sun} onClick={() => setMode('light')}>
          Light
        </ChoiceButton>
        <ChoiceButton selected={mode === 'dark'} icon={Moon} onClick={() => setMode('dark')}>
          Dark
        </ChoiceButton>
      </div>
      <h3 className="mb-2 text-sm font-semibold">Accent colour</h3>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6" role="radiogroup" aria-label="Accent colour">
        {Object.entries(THEME_PRESETS).map(([key, preset]) => (
          <button
            key={key}
            type="button"
            role="radio"
            aria-checked={theme === key}
            onClick={() => setTheme(key)}
            className={cn(
              'group relative flex flex-col items-center gap-2 rounded-2xl border-2 p-4 transition-[border-color,box-shadow] hover:shadow-sm',
              theme === key ? 'border-primary bg-accent/50 shadow-xs' : 'border-border bg-card hover:border-primary/40'
            )}
          >
            <span className="flex size-10 items-center justify-center rounded-full shadow-sm" style={{ background: preset.primary }}>
              <Palette className="size-5 text-white" aria-hidden="true" />
            </span>
            <span className="text-sm font-semibold">{preset.label}</span>
            {theme === key && (
              <span className="absolute top-2 right-2 flex size-5 items-center justify-center rounded-full bg-primary text-primary-foreground">
                <Check className="size-3" aria-hidden="true" />
              </span>
            )}
          </button>
        ))}
      </div>
    </SectionCard>
  );
}
