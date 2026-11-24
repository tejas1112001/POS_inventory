import { createSlice } from '@reduxjs/toolkit';

// UI state (Teal & Cream theme): light/dark mode, the accent preset and the
// sidebar. Mode and accent are kept in this browser (see store/index.js).
export const ACCENTS = {
  teal: { label: 'Teal', primary: '#17807a' },
  ocean: { label: 'Ocean', primary: '#1f6f8b' },
  blue: { label: 'Blue', primary: '#2563eb' },
  violet: { label: 'Violet', primary: 'oklch(0.541 0.22 293)' },
  rose: { label: 'Rose', primary: 'oklch(0.567 0.197 15)' },
  amber: { label: 'Amber', primary: 'oklch(0.556 0.149 58)' },
};

export const STORAGE = { accent: 'reactronpos-theme', mode: 'mintobill-mode', sidebar: 'mintobill-sidebar' };

function read(key) {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

// 'skyblue' and 'blue' were the old defaults; teal replaces them.
function initialAccent() {
  const saved = read(STORAGE.accent);
  return saved && saved !== 'skyblue' && saved !== 'blue' && ACCENTS[saved] ? saved : 'teal';
}

const uiSlice = createSlice({
  name: 'ui',
  initialState: () => ({
    mode: read(STORAGE.mode) === 'dark' ? 'dark' : 'light',
    accent: initialAccent(),
    sidebarCollapsed: read(STORAGE.sidebar) === '1',
    mobileNavOpen: false,
  }),
  reducers: {
    setMode(state, { payload }) {
      state.mode = payload === 'dark' ? 'dark' : 'light';
    },
    toggleMode(state) {
      state.mode = state.mode === 'dark' ? 'light' : 'dark';
    },
    setAccent(state, { payload }) {
      if (ACCENTS[payload]) state.accent = payload;
    },
    setSidebarCollapsed(state, { payload }) {
      state.sidebarCollapsed = !!payload;
    },
    setMobileNavOpen(state, { payload }) {
      state.mobileNavOpen = !!payload;
    },
  },
});

export const { setMode, toggleMode, setAccent, setSidebarCollapsed, setMobileNavOpen } = uiSlice.actions;
export default uiSlice.reducer;

export const selectMode = (s) => s.ui.mode;
export const selectAccent = (s) => s.ui.accent;
export const selectSidebarCollapsed = (s) => s.ui.sidebarCollapsed;
export const selectMobileNavOpen = (s) => s.ui.mobileNavOpen;
