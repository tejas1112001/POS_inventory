import { useEffect, useState } from 'react';

/** Live result of a CSS media query, e.g. useMedia('(prefers-reduced-motion: reduce)'). */
export function useMedia(query) {
  // Starts false, as in the HTML built ahead of time; the effect reads the real value.
  const [match, setMatch] = useState(false);
  useEffect(() => {
    const mq = window.matchMedia(query);
    const on = () => setMatch(mq.matches);
    on();
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, [query]);
  return match;
}
