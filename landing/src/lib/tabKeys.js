/**
 * Arrow-key, Home and End navigation for an ARIA tablist (WAI-ARIA tabs
 * pattern, automatic activation). Put the returned handler on the tablist
 * element and give each tab a `data-tab` attribute with its id.
 */
export function tabKeyHandler(ids, active, onChange) {
  return (e) => {
    const i = ids.indexOf(active);
    let next = null;
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = ids[(i + 1) % ids.length];
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = ids[(i - 1 + ids.length) % ids.length];
    else if (e.key === 'Home') next = ids[0];
    else if (e.key === 'End') next = ids[ids.length - 1];
    if (next == null) return;
    e.preventDefault();
    const list = e.currentTarget;
    onChange(next);
    requestAnimationFrame(() => list.querySelector(`[data-tab="${next}"]`)?.focus());
  };
}
