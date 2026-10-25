// Money and number formatting from the tenant's settings (GET /api/auth/me:
// currency and an English number-format locale such as en-IN). All UI text
// stays English; only the number format follows the locale.

let settings = { currency: 'INR', locale: 'en-IN' };
const cache = new Map();

function validLocale(locale) {
  return typeof locale === 'string' && /^en-[A-Z]{2}$/.test(locale) ? locale : 'en-IN';
}

function validCurrency(currency) {
  return typeof currency === 'string' && /^[A-Z]{3}$/.test(currency) ? currency : 'INR';
}

export function setFormatSettings({ currency, locale } = {}) {
  settings = { currency: validCurrency(currency), locale: validLocale(locale) };
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

export function initials(name) {
  if (!name) return '';
  return name
    .split(' ')
    .filter(Boolean)
    .slice(0, 2)
    .map((s) => s[0]?.toUpperCase())
    .join('');
}
