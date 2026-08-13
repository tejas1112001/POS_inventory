import React, { createContext, useContext, useEffect, useState } from 'react';

export const THEME_PRESETS = {
  skyblue: {
    label: 'Sky Blue',
    primary: 'oklch(0.588 0.158 230)',
    primaryForeground: 'oklch(0.985 0.01 230)',
    ring: 'oklch(0.588 0.158 230)',
    accent: 'oklch(0.94 0.04 230)',
    accentForeground: 'oklch(0.35 0.1 230)',
    chart1: 'oklch(0.588 0.158 230)',
  },
  emerald: {
    label: 'Emerald Green',
    primary: 'oklch(0.577 0.165 162)',
    primaryForeground: 'oklch(0.985 0.01 165)',
    ring: 'oklch(0.577 0.165 162)',
    accent: 'oklch(0.95 0.03 165)',
    accentForeground: 'oklch(0.35 0.08 162)',
    chart1: 'oklch(0.577 0.165 162)',
  },
  violet: {
    label: 'Violet',
    primary: 'oklch(0.541 0.22 293)',
    primaryForeground: 'oklch(0.985 0.01 293)',
    ring: 'oklch(0.541 0.22 293)',
    accent: 'oklch(0.94 0.04 293)',
    accentForeground: 'oklch(0.35 0.12 293)',
    chart1: 'oklch(0.541 0.22 293)',
  },
  rose: {
    label: 'Rose',
    primary: 'oklch(0.577 0.2 15)',
    primaryForeground: 'oklch(0.985 0.01 15)',
    ring: 'oklch(0.577 0.2 15)',
    accent: 'oklch(0.94 0.04 15)',
    accentForeground: 'oklch(0.35 0.12 15)',
    chart1: 'oklch(0.577 0.2 15)',
  },
  amber: {
    label: 'Amber',
    primary: 'oklch(0.666 0.179 58)',
    primaryForeground: 'oklch(0.985 0.01 58)',
    ring: 'oklch(0.666 0.179 58)',
    accent: 'oklch(0.94 0.04 58)',
    accentForeground: 'oklch(0.35 0.1 58)',
    chart1: 'oklch(0.666 0.179 58)',
  },
};

const STORAGE_KEY = 'reactronpos-theme';

const ThemeContext = createContext();

function applyTheme(presetKey) {
  const preset = THEME_PRESETS[presetKey] || THEME_PRESETS.skyblue;
  const root = document.documentElement;

  root.style.setProperty('--primary', preset.primary);
  root.style.setProperty('--primary-foreground', preset.primaryForeground);
  root.style.setProperty('--ring', preset.ring);
  root.style.setProperty('--accent', preset.accent);
  root.style.setProperty('--accent-foreground', preset.accentForeground);
  root.style.setProperty('--chart-1', preset.chart1);
  root.style.setProperty('--sidebar-primary', preset.primary);
  root.style.setProperty('--sidebar-primary-foreground', preset.primaryForeground);
  root.style.setProperty('--sidebar-accent', preset.accent);
  root.style.setProperty('--sidebar-accent-foreground', preset.accentForeground);
  root.style.setProperty('--sidebar-ring', preset.ring);
  root.dataset.theme = presetKey;
}

export function ThemeProvider({ children }) {
  const [theme, setThemeState] = useState(() => {
    return localStorage.getItem(STORAGE_KEY) || 'skyblue';
  });

  useEffect(() => {
    applyTheme(theme);
    localStorage.setItem(STORAGE_KEY, theme);
  }, [theme]);

  const setTheme = (presetKey) => {
    if (THEME_PRESETS[presetKey]) {
      setThemeState(presetKey);
    }
  };

  return (
    <ThemeContext.Provider value={{ theme, setTheme, presets: THEME_PRESETS }}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme() {
  const ctx = useContext(ThemeContext);
  if (!ctx) {
    throw new Error('useTheme must be used within ThemeProvider');
  }
  return ctx;
}
