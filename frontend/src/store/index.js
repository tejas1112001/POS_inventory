import { configureStore } from '@reduxjs/toolkit';
import ui, { STORAGE } from './uiSlice';

export const store = configureStore({ reducer: { ui } });

function write(key, value) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* private window: not kept */
  }
}

// Keep the UI preferences in this browser; write only when they change.
let last = store.getState().ui;
store.subscribe(() => {
  const ui = store.getState().ui;
  if (ui === last) return;
  if (ui.mode !== last.mode) write(STORAGE.mode, ui.mode);
  if (ui.accent !== last.accent) write(STORAGE.accent, ui.accent);
  if (ui.sidebarCollapsed !== last.sidebarCollapsed) write(STORAGE.sidebar, ui.sidebarCollapsed ? '1' : '0');
  last = ui;
});
