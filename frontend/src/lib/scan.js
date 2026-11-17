// Barcode scanning (industry P1 item 3, D1). A USB or Bluetooth scanner in
// keyboard mode "types" the code very fast and ends it with Enter (or Tab).
// A run of keystrokes that arrive less than `maxGap` ms apart and end with
// Enter or Tab is a scan; slower typing is a person. Typing a code by hand
// and pressing Enter still works, because Enter does an exact match.

export const DEFAULT_GAP_MS = 30;
const GAP_KEY = 'pos.scanGapMs';
export const MIN_SCAN_LENGTH = 4;

// scanGap is the per-browser gap threshold (some Bluetooth scanners type
// slower). Stored in localStorage; any failure falls back to the default.
export function scanGap() {
  try {
    const v = Number(globalThis.localStorage?.getItem(GAP_KEY));
    return v >= 10 && v <= 200 ? v : DEFAULT_GAP_MS;
  } catch {
    return DEFAULT_GAP_MS;
  }
}

export function setScanGap(ms) {
  try {
    globalThis.localStorage?.setItem(GAP_KEY, String(ms));
  } catch {
    // private window or blocked storage: keep the default
  }
}

// createScanDetector keeps the recent keystrokes. feed(key, time) returns
// the scanned code when key ends a scan (Enter or Tab after a fast run of
// at least minLength printable characters), else null. Any key that is not
// a single printable character, or a gap longer than maxGap, starts over.
export function createScanDetector({ maxGap = DEFAULT_GAP_MS, minLength = MIN_SCAN_LENGTH } = {}) {
  let buf = '';
  let last = -Infinity;
  let firstAt = -Infinity;
  return {
    feed(key, time) {
      const gap = time - last;
      if (key === 'Enter' || key === 'Tab') {
        const code = buf;
        const fast = code.length >= minLength && gap <= maxGap * 4; // the suffix may follow a little later
        buf = '';
        last = -Infinity;
        return fast ? code : null;
      }
      if (typeof key !== 'string' || key.length !== 1) {
        buf = '';
        last = -Infinity;
        return null;
      }
      if (buf && gap > maxGap) buf = '';
      if (!buf) firstAt = time;
      buf += key;
      last = time;
      return null;
    },
    // pending: characters typed fast so far (a scan may be under way).
    pending() {
      return buf;
    },
    startedAt() {
      return firstAt;
    },
    reset() {
      buf = '';
      last = -Infinity;
    },
  };
}

// findByCode finds the variant of a scanned or typed code in a product
// list (checkout loads every variant's barcode and SKU): an exact barcode
// first, then an exact SKU (SKUs are case-insensitive). Returns
// { product, variant } or null.
export function findByCode(products, code, variantsOf = (p) => p?.ProductVariants || []) {
  const c = String(code || '').trim();
  if (!c) return null;
  for (const p of products || []) {
    for (const v of variantsOf(p)) {
      if (v.itemBarcode && v.itemBarcode === c) return { product: p, variant: v };
    }
  }
  const lower = c.toLowerCase();
  for (const p of products || []) {
    for (const v of variantsOf(p)) {
      if (v.sku && v.sku.toLowerCase() === lower) return { product: p, variant: v };
    }
  }
  return null;
}

// beep plays a short low tone for "no match" (Web Audio; silent when the
// browser blocks audio).
let audioCtx = null;
export function beep() {
  try {
    const Ctx = globalThis.AudioContext || globalThis.webkitAudioContext;
    if (!Ctx) return;
    audioCtx = audioCtx || new Ctx();
    const osc = audioCtx.createOscillator();
    const gain = audioCtx.createGain();
    osc.type = 'square';
    osc.frequency.value = 220;
    gain.gain.value = 0.08;
    osc.connect(gain).connect(audioCtx.destination);
    osc.start();
    osc.stop(audioCtx.currentTime + 0.18);
  } catch {
    // no audio: the toast still says it
  }
}
