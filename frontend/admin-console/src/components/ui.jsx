import { useEffect, useId, useRef } from 'react';
import styles from './Modal.module.css';

// Small UI kit for the console. Everything renders text through React, so
// tenant-supplied values are always escaped.

export function Button({ variant, busy, children, type = 'button', ...rest }) {
  const cls = ['btn', variant && `btn-${variant}`].filter(Boolean).join(' ');
  return (
    <button type={type} className={cls} disabled={busy || rest.disabled} {...rest}>
      {busy ? 'Working…' : children}
    </button>
  );
}

export function Field({ label, hint, error, children }) {
  return (
    <label className="field">
      <span>{label}</span>
      {children}
      {error ? <span className="error-text">{error}</span> : hint ? <span className="hint">{hint}</span> : null}
    </label>
  );
}

export function TextInput({ label, hint, error, value, onChange, ...rest }) {
  return (
    <Field label={label} hint={hint} error={error}>
      <input className="input" value={value ?? ''} onChange={(e) => onChange(e.target.value)} aria-invalid={!!error} {...rest} />
    </Field>
  );
}

export function TextArea({ label, hint, error, value, onChange, ...rest }) {
  return (
    <Field label={label} hint={hint} error={error}>
      <textarea className="textarea" value={value ?? ''} onChange={(e) => onChange(e.target.value)} aria-invalid={!!error} {...rest} />
    </Field>
  );
}

export function Select({ label, hint, error, value, onChange, options, ...rest }) {
  return (
    <Field label={label} hint={hint} error={error}>
      <select className="select" value={value ?? ''} onChange={(e) => onChange(e.target.value)} {...rest}>
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </Field>
  );
}

export function Check({ label, checked, onChange, ...rest }) {
  return (
    <label className="check">
      <input type="checkbox" checked={!!checked} onChange={(e) => onChange(e.target.checked)} {...rest} />
      <span>{label}</span>
    </label>
  );
}

export function Notice({ kind = 'info', children }) {
  if (!children) return null;
  return (
    <div role={kind === 'error' ? 'alert' : 'status'} className={`notice notice-${kind}`}>
      {children}
    </div>
  );
}

const STATUS_KIND = {
  active: 'ok',
  trialing: 'ok',
  succeeded: 'ok',
  paid: 'ok',
  past_due: 'warn',
  grace: 'warn',
  cancelled: 'warn',
  queued: 'muted',
  running: 'warn',
  provisioning: 'warn',
  issued: 'warn',
  read_only: 'danger',
  suspended: 'danger',
  pending_deletion: 'danger',
  failed: 'danger',
  deleted: 'muted',
  expired: 'muted',
};

export function StatusBadge({ status }) {
  if (!status) return null;
  const kind = STATUS_KIND[status] || 'muted';
  return <span className={`badge badge-${kind}`}>{String(status).replace(/_/g, ' ')}</span>;
}

export function Tabs({ tabs, value, onChange }) {
  return (
    <div role="tablist" className="tabs">
      {tabs.map((t) => (
        <button key={t.key} type="button" role="tab" className="tab" aria-selected={value === t.key} onClick={() => onChange(t.key)}>
          {t.label}
        </button>
      ))}
    </div>
  );
}

// Table renders rows with columns {key, label, render?, className?}.
export function Table({ columns, rows, rowKey = (r) => r.id, onRowClick, empty = 'Nothing here.', loading }) {
  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c.key} className={c.className}>
                {c.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr
              key={rowKey(r)}
              className={onRowClick ? 'clickable' : undefined}
              onClick={onRowClick ? () => onRowClick(r) : undefined}
              onKeyDown={onRowClick ? (e) => e.key === 'Enter' && onRowClick(r) : undefined}
              tabIndex={onRowClick ? 0 : undefined}
            >
              {columns.map((c) => (
                <td key={c.key} className={c.className}>
                  {c.render ? c.render(r) : r[c.key]}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {loading ? <div className="empty">Loading…</div> : rows.length === 0 ? <div className="empty">{empty}</div> : null}
    </div>
  );
}

export function Pager({ page, totalPages, onPage }) {
  if (!totalPages || totalPages <= 1) return null;
  return (
    <div className="row right">
      <Button onClick={() => onPage(page - 1)} disabled={page <= 1}>
        Previous
      </Button>
      <span className="muted">
        Page {page} of {totalPages}
      </span>
      <Button onClick={() => onPage(page + 1)} disabled={page >= totalPages}>
        Next
      </Button>
    </div>
  );
}

export function Modal({ title, onClose, children, footer, wide }) {
  const titleId = useId();
  const box = useRef(null);
  useEffect(() => {
    const prev = document.activeElement;
    box.current?.querySelector('input, select, textarea, button')?.focus();
    const onKey = (e) => e.key === 'Escape' && onClose();
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('keydown', onKey);
      prev?.focus?.();
    };
  }, [onClose]);
  return (
    <div className={styles.overlay} onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} role="dialog" aria-modal="true" aria-labelledby={titleId} className={`${styles.box} ${wide ? styles.wide : ''}`}>
        <div className={styles.head}>
          <h2 id={titleId}>{title}</h2>
          <button type="button" className={styles.close} onClick={onClose} aria-label="Close">
            ×
          </button>
        </div>
        <div className={styles.body}>{children}</div>
        {footer && <div className={styles.foot}>{footer}</div>}
      </div>
    </div>
  );
}

export function Facts({ items }) {
  return (
    <dl className="facts">
      {items
        .filter(([, v]) => v !== undefined && v !== null && v !== '')
        .map(([k, v]) => (
          <div key={k} className="contents">
            <dt>{k}</dt>
            <dd>{v}</dd>
          </div>
        ))}
    </dl>
  );
}
