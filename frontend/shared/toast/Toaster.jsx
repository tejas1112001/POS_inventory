import { useEffect, useRef, useState } from 'react';
import { DURATION, dismiss, subscribe } from './store';

const ICON = {
  success: 'M20 6 9 17l-5-5',
  info: 'M12 16v-4m0-4h.01M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0Z',
  warning: 'M12 9v4m0 4h.01M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z',
  error: 'M12 8v4m0 4h.01M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0Z',
};

function Icon({ kind }) {
  return (
    <svg className="toast__icon" viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d={ICON[kind]} />
    </svg>
  );
}

// One toast: its own timer, paused while hovered or focused.
function Item({ t }) {
  const [paused, setPaused] = useState(false);
  const left = useRef(t.sticky ? 0 : DURATION[t.kind]);
  const started = useRef(Date.now());

  useEffect(() => {
    left.current = t.sticky ? 0 : DURATION[t.kind];
  }, [t.at, t.kind, t.sticky]);

  useEffect(() => {
    if (paused || !left.current) return undefined;
    started.current = Date.now();
    const timer = setTimeout(() => dismiss(t.id), left.current);
    return () => {
      clearTimeout(timer);
      left.current = Math.max(0, left.current - (Date.now() - started.current));
    };
  }, [paused, t.id, t.at]);

  return (
    <li
      className={`toast toast--${t.kind}`}
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocus={() => setPaused(true)}
      onBlur={() => setPaused(false)}
      onKeyDown={(e) => e.key === 'Escape' && dismiss(t.id)}
    >
      <Icon kind={t.kind} />
      <p className="toast__text">
        {t.message}
        {t.count > 1 && <span className="toast__count"> ×{t.count}</span>}
      </p>
      {t.action && (
        <button
          type="button"
          className="toast__action"
          onClick={() => {
            t.action.onClick?.();
            dismiss(t.id);
          }}
        >
          {t.action.label}
        </button>
      )}
      <button type="button" className="toast__close" aria-label="Close message" onClick={() => dismiss(t.id)}>
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
          <path d="M18 6 6 18M6 6l12 12" />
        </svg>
      </button>
    </li>
  );
}

// Toaster renders the messages: errors in an assertive live region,
// everything else in a polite one. Toasts never take focus.
export default function Toaster() {
  const [items, setItems] = useState([]);
  useEffect(() => subscribe(setItems), []);
  const urgent = items.filter((t) => t.kind === 'error');
  const calm = items.filter((t) => t.kind !== 'error');
  return (
    <div className="toast-viewport">
      <div role="status" aria-live="polite" aria-label="Notifications">
        <ol className="toast-list">
          {calm.map((t) => (
            <Item key={t.id} t={t} />
          ))}
        </ol>
      </div>
      <div role="alert" aria-live="assertive" aria-label="Errors">
        <ol className="toast-list">
          {urgent.map((t) => (
            <Item key={t.id} t={t} />
          ))}
        </ol>
      </div>
    </div>
  );
}
