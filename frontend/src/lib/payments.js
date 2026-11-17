import { formatDate } from './format';

// Payment methods as the server stores them (S10 item 5). "credit" is the
// part of a sale put on the customer's account.
export const METHOD_LABELS = {
  cash: 'Cash',
  upi: 'UPI',
  card: 'Card',
  credit: 'On account',
  cheque: 'Cheque',
  deposit: 'Bank deposit',
  points: 'Points',
  exchange: 'Exchange',
  store_credit: 'Store credit',
};

// Methods that can pay dues later (no on-account, no points).
export const COLLECT_METHODS = ['cash', 'upi', 'card', 'cheque', 'deposit'];

export const methodLabel = (m) => METHOD_LABELS[m] || m || '';

// Industry P1 item 2 (D3, Q4): payments are recorded by the cashier and
// never verified by a bank or provider in this release. Every method
// except cash (and the on-account part and points, which are not money
// taken) is labelled "(recorded)".
export const RECORDED_METHODS = ['upi', 'card', 'cheque', 'deposit', 'exchange', 'store_credit'];
export const isRecorded = (m) => RECORDED_METHODS.includes(m);
export const RECORDED_HINT = 'Recorded by the cashier, not verified by a bank or payment provider.';
export const RECORDED_LEGEND = 'Payments are recorded by the cashier and are not verified by a bank or payment provider.';
export const paymentLabel = (m) => `${methodLabel(m)}${isRecorded(m) ? ' (recorded)' : ''}`;

// newIdempotencyKey for one-off money actions (collect a payment).
export function newKey() {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID();
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

// formatDay shows a calendar day (YYYY-MM-DD, such as a due date) without
// moving it across midnight in the shop's time zone.
export function formatDay(day) {
  return day ? formatDate(`${day}T12:00:00Z`) : '';
}

// addDays returns YYYY-MM-DD n days after a YYYY-MM-DD day.
export function addDays(day, n) {
  const d = new Date(`${day}T12:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
}
