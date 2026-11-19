import { useEffect, useState } from 'react';

/**
 * True once the page has loaded and the browser is idle. Used to hold back
 * decorative effects so they never compete with the first paint.
 */
export function useIdle(timeout = 1500) {
  const [idle, setIdle] = useState(false);
  useEffect(() => {
    let handle;
    let cancelIdle = () => {};
    const whenIdle = () => {
      if ('requestIdleCallback' in window) {
        handle = window.requestIdleCallback(() => setIdle(true), { timeout });
        cancelIdle = () => window.cancelIdleCallback(handle);
      } else {
        handle = window.setTimeout(() => setIdle(true), 200);
        cancelIdle = () => window.clearTimeout(handle);
      }
    };
    if (document.readyState === 'complete') whenIdle();
    else window.addEventListener('load', whenIdle, { once: true });
    return () => {
      window.removeEventListener('load', whenIdle);
      cancelIdle();
    };
  }, [timeout]);
  return idle;
}
