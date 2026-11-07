// Toast store shared by the shop app and the Super Admin console. Plain
// JavaScript: no styles are created at run time (strict CSP in the console).

// Durations (ms) per kind; 0 means "stays until closed".
export const DURATION = { success: 4000, info: 5000, warning: 7000, error: 8000 };
const MAX = 3;
const MERGE_WINDOW = 3000;

let items = []; // { id, kind, message, action, sticky, count, at }
let seq = 0;
const listeners = new Set();

function emit() {
  for (const l of listeners) l(items);
}

export function subscribe(fn) {
  listeners.add(fn);
  fn(items);
  return () => listeners.delete(fn);
}

export function dismiss(id) {
  items = items.filter((t) => t.id !== id);
  emit();
}

// show adds a toast. options: { action: { label, onClick }, sticky }.
// An identical message within 3 s is merged ("×2") instead of stacked.
function show(kind, message, options = {}) {
  const text = String(message || '').trim();
  if (!text) return null;
  const now = Date.now();
  const same = items.find((t) => t.kind === kind && t.message === text && now - t.at < MERGE_WINDOW);
  if (same) {
    items = items.map((t) => (t === same ? { ...t, count: t.count + 1, at: now } : t));
    emit();
    return same.id;
  }
  const id = ++seq;
  const toast = { id, kind, message: text, action: options.action || null, sticky: !!options.sticky, count: 1, at: now };
  items = [...items, toast].slice(-MAX);
  emit();
  return id;
}

export const toast = {
  success: (m, o) => show('success', m, o),
  info: (m, o) => show('info', m, o),
  warning: (m, o) => show('warning', m, o),
  // Errors that need the user to act should pass { sticky: true }.
  error: (m, o) => show('error', m, o),
  dismiss,
};
