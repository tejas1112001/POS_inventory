// Dates in the viewer's time zone, English formats.
const dt = new Intl.DateTimeFormat('en-GB', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' });
const d = new Intl.DateTimeFormat('en-GB', { day: '2-digit', month: 'short', year: 'numeric' });

function parse(v) {
  if (!v) return null;
  const x = new Date(v);
  return Number.isNaN(x.getTime()) ? null : x;
}

export const formatDateTime = (v, fallback = '-') => (parse(v) ? dt.format(parse(v)) : fallback);
export const formatDate = (v, fallback = '-') => (parse(v) ? d.format(parse(v)) : fallback);

// Plan prices and invoices are in minor units (paise, cents).
export function money(minor, currency = 'INR') {
  try {
    return new Intl.NumberFormat('en-IN', { style: 'currency', currency }).format((Number(minor) || 0) / 100);
  } catch {
    return `${((Number(minor) || 0) / 100).toFixed(2)} ${currency}`;
  }
}

export function bytes(n) {
  const v = Number(n) || 0;
  if (v < 1024) return `${v} B`;
  if (v < 1024 * 1024) return `${(v / 1024).toFixed(1)} KB`;
  if (v < 1024 ** 3) return `${(v / 1024 / 1024).toFixed(1)} MB`;
  return `${(v / 1024 ** 3).toFixed(2)} GB`;
}
