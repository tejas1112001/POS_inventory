import { useEffect, useRef } from 'react';
import { createScanDetector, scanGap } from '@/lib/scan';

// isEditable: an input where typed characters land (a scan there is the
// field's own business unless the screen accepts it).
function isEditable(el) {
  if (!el) return false;
  const tag = el.tagName;
  return el.isContentEditable || tag === 'TEXTAREA' || tag === 'SELECT' || (tag === 'INPUT' && !['checkbox', 'radio', 'button', 'submit'].includes(el.type));
}

// useScanner listens for barcode scans anywhere on the page (industry P1
// item 3). onScan(code, target) is called when a fast run of keystrokes
// ends with Enter or Tab; that Enter/Tab is swallowed so it does not submit
// a form or move focus. Scans typed into an editable field are only taken
// when accept(target) says so (for example the screen's search box);
// elsewhere they are left to the field.
export function useScanner(onScan, { enabled = true, accept } = {}) {
  const handler = useRef(onScan);
  const acceptRef = useRef(accept);
  useEffect(() => {
    handler.current = onScan;
    acceptRef.current = accept;
  });
  useEffect(() => {
    if (!enabled) return undefined;
    const detector = createScanDetector({ maxGap: scanGap() });
    const onKey = (e) => {
      if (e.ctrlKey || e.metaKey || e.altKey) {
        detector.reset();
        return;
      }
      const code = detector.feed(e.key, e.timeStamp || performance.now());
      if (!code) return;
      const t = e.target;
      if (isEditable(t) && !(acceptRef.current && acceptRef.current(t))) return;
      e.preventDefault();
      e.stopPropagation();
      handler.current?.(code, t);
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [enabled]);
}
