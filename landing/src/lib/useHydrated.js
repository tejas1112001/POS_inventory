import { useSyncExternalStore } from 'react';

const subscribe = () => () => {};

/**
 * False while the page is rendered at build time and during hydration,
 * true afterwards. The strict CSP (style-src 'self') blocks style=""
 * attributes in HTML, so inline styles (chart bar heights and the like) are
 * set only once React runs in the browser, where they are applied through
 * the DOM and allowed.
 */
export function useHydrated() {
  return useSyncExternalStore(subscribe, () => true, () => false);
}
