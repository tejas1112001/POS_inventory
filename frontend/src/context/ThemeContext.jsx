import { useEffect } from 'react';
import { Provider } from 'react-redux';
import { store } from '@/store';
import { useAppDispatch, useAppSelector } from '@/store/hooks';
import { ACCENTS, selectAccent, selectMode, setAccent, setMode, toggleMode } from '@/store/uiSlice';

// Theme state lives in the Redux store (store/uiSlice.js). Accent presets
// change the accent colour only (buttons, active items, links); tints are
// derived from it in index.css, so every preset works in light and dark
// mode. Every primary passes 4.5:1 with white text (WCAG AA).
export const THEME_PRESETS = ACCENTS;

// ThemeSync applies the current mode and accent to <html>.
function ThemeSync() {
  const mode = useAppSelector(selectMode);
  const accent = useAppSelector(selectAccent);

  useEffect(() => {
    const preset = ACCENTS[accent] || ACCENTS.teal;
    const root = document.documentElement;
    root.style.setProperty('--primary', preset.primary);
    root.style.setProperty('--ring', preset.primary);
    root.style.setProperty('--chart-1', accent === 'teal' ? '#20b6a4' : preset.primary);
    root.dataset.theme = accent;
  }, [accent]);

  useEffect(() => {
    const root = document.documentElement;
    root.classList.toggle('dark', mode === 'dark');
    root.classList.toggle('light', mode !== 'dark');
  }, [mode]);

  // Leaving the staff app (e.g. to the public shop) drops the mode class.
  useEffect(() => () => document.documentElement.classList.remove('dark', 'light'), []);
  return null;
}

// ThemeProvider: the Redux store for the staff app plus the theme sync.
export function ThemeProvider({ children }) {
  return (
    <Provider store={store}>
      <ThemeSync />
      {children}
    </Provider>
  );
}

// useTheme: the theme API the screens use (read from and written to Redux).
export function useTheme() {
  const dispatch = useAppDispatch();
  const mode = useAppSelector(selectMode);
  const theme = useAppSelector(selectAccent);
  return {
    theme,
    presets: ACCENTS,
    mode,
    setTheme: (key) => dispatch(setAccent(key)),
    setMode: (m) => dispatch(setMode(m)),
    toggleMode: () => dispatch(toggleMode()),
  };
}
