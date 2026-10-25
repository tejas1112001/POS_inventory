// Money and number formatting from the tenant's settings (GET /api/auth/me:
// currency and an English number-format locale such as en-IN). All UI text
// stays English; only the number format follows the locale.

let settings = { currency: 'INR', locale: 'en-IN', timezone: undefined };
const cache = new Map();

function validLocale(locale) {
  return typeof locale === 'string' && /^en-[A-Z]{2}$/.test(locale) ? locale : 'en-IN';
}

function validCurrency(currency) {
  return typeof currency === 'string' && /^[A-Z]{3}$/.test(currency) ? currency : 'INR';
}

function validTimeZone(tz) {
  if (typeof tz !== 'string' || !tz) return undefined;
  try {
    new Intl.DateTimeFormat('en', { timeZone: tz });
    return tz;
  } catch {
    return undefined;
  }
}

export function setFormatSettings({ currency, locale, timezone } = {}) {
  settings = { currency: validCurrency(currency), locale: validLocale(locale), timezone: validTimeZone(timezone) };
  cache.clear();
}

export function getFormatSettings() {
  return settings;
}

function currencyFormatter() {
  const key = `c:${settings.locale}:${settings.currency}`;
  if (!cache.has(key)) {
    let f;
    try {
      f = new Intl.NumberFormat(settings.locale, { style: 'currency', currency: settings.currency });
    } catch {
      f = new Intl.NumberFormat('en-IN', { style: 'currency', currency: 'INR' });
    }
    cache.set(key, f);
  }
  return cache.get(key);
}

export function formatCurrency(value) {
  return currencyFormatter().format(Number(value) || 0);
}

// currencySymbol is the symbol the tenant's currency uses (₹, $, …).
export function currencySymbol() {
  const part = currencyFormatter()
    .formatToParts(0)
    .find((p) => p.type === 'currency');
  return part ? part.value : settings.currency;
}

export function formatNumber(value, maximumFractionDigits = 2) {
  return new Intl.NumberFormat(settings.locale, { maximumFractionDigits }).format(Number(value) || 0);
}

function dateFormatter(key, options) {
  const k = `d:${key}:${settings.locale}:${settings.timezone}`;
  if (!cache.has(k)) cache.set(k, new Intl.DateTimeFormat(settings.locale, { ...options, timeZone: settings.timezone }));
  return cache.get(k);
}

function toDate(value) {
  if (value === null || value === undefined || value === '') return null;
  const d = value instanceof Date ? value : new Date(value);
  return Number.isNaN(d.getTime()) ? null : d;
}

// formatDate shows a date in the shop's time zone ("25 Oct 2026").
export function formatDate(value, fallback = '') {
  const d = toDate(value);
  return d ? dateFormatter('date', { day: '2-digit', month: 'short', year: 'numeric' }).format(d) : fallback;
}

// formatDateTime shows a date and time in the shop's time zone.
export function formatDateTime(value, fallback = '') {
  const d = toDate(value);
  return d
    ? dateFormatter('datetime', { day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' }).format(d)
    : fallback;
}

export function initials(name) {
  if (!name) return '';
  return name
    .split(' ')
    .filter(Boolean)
    .slice(0, 2)
    .map((s) => s[0]?.toUpperCase())
    .join('');
}
