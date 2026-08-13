import React from 'react';
import { Palette, Check } from 'lucide-react';
import { SectionCard } from '@/components/shared/section-card';
import { useTheme, THEME_PRESETS } from '@/context/ThemeContext';
import { cn } from '@/lib/utils';

export default function Settings() {
  const { theme, setTheme } = useTheme();

  return (
    <div className="flex h-full flex-col gap-3 p-3 sm:p-4">
      <SectionCard title="Theme Color">
        <p className="mb-4 text-sm text-muted-foreground">
          Choose an accent color for buttons, sidebar highlights, and form actions across the app.
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
    </div>
  );
}
