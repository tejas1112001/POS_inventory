import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';

const typing = (el) => el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName));

// useShortcuts: "g" then d / p / o opens Dashboard, Checkout (POS) or
// Orders; "/" focuses the page's search box. Ignored while typing.
export default function useShortcuts() {
  const navigate = useNavigate();
  useEffect(() => {
    let pendingG = 0;
    const onKey = (e) => {
      if (e.ctrlKey || e.metaKey || e.altKey || typing(document.activeElement)) return;
      if (e.key === '/') {
        const search = document.querySelector('main input[type="search"], main input[placeholder^="Search"]');
        if (search) {
          e.preventDefault();
          search.focus();
        }
        return;
      }
      if (e.key === 'g') {
        pendingG = Date.now();
        return;
      }
      if (Date.now() - pendingG < 1000) {
        const to = { d: '/dashboard', p: '/pos', o: '/sales' }[e.key];
        pendingG = 0;
        if (to) {
          e.preventDefault();
          navigate(to);
        }
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [navigate]);
}
