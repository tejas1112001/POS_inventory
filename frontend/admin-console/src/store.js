import { configureStore, createSlice } from '@reduxjs/toolkit';

// Console UI state (Redux): light / dark mode and the phone menu. The mode
// follows the system until the user picks one; the choice is kept in this
// browser.
const MODE_KEY = 'console.mode';

function readMode() {
  try {
    const m = localStorage.getItem(MODE_KEY);
    return m === 'light' || m === 'dark' ? m : 'system';
  } catch {
    return 'system';
  }
}

function systemDark() {
  return typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: dark)').matches;
}

const ui = createSlice({
  name: 'ui',
  initialState: () => ({ mode: readMode(), menuOpen: false }),
  reducers: {
    toggleMode(state) {
      const dark = state.mode === 'dark' || (state.mode === 'system' && systemDark());
      state.mode = dark ? 'light' : 'dark';
    },
    setMenuOpen(state, { payload }) {
      state.menuOpen = !!payload;
    },
  },
});

export const { toggleMode, setMenuOpen } = ui.actions;
export const selectMode = (s) => s.ui.mode;
export const selectMenuOpen = (s) => s.ui.menuOpen;
export const isDark = (mode) => mode === 'dark' || (mode === 'system' && systemDark());

export const store = configureStore({ reducer: { ui: ui.reducer } });

// Apply the mode to <html> and keep it.
let last;
function apply() {
  const { mode } = store.getState().ui;
  if (mode === last) return;
  last = mode;
  const root = document.documentElement;
  root.classList.toggle('dark', mode === 'dark');
  root.classList.toggle('light', mode === 'light');
  try {
    if (mode === 'system') localStorage.removeItem(MODE_KEY);
    else localStorage.setItem(MODE_KEY, mode);
  } catch {
    /* private window: not kept */
  }
}
apply();
store.subscribe(apply);
