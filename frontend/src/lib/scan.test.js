// Unit tests for the scan detector (industry P1 item 3). Run with
// `npm run test:unit` (Node's built-in test runner, no dependencies).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createScanDetector, findByCode } from './scan.js';

function type(det, text, gap, suffix = 'Enter', start = 1000) {
  let t = start;
  let out = null;
  for (const ch of text) {
    out = det.feed(ch, t) ?? out;
    t += gap;
  }
  return det.feed(suffix, t - gap + Math.min(gap, 5)) ?? out;
}

test('a fast run ending in Enter is a scan', () => {
  assert.equal(type(createScanDetector(), '8901234567890', 8), '8901234567890');
});

test('a Tab suffix also ends a scan', () => {
  assert.equal(type(createScanDetector(), 'ABC-123', 10, 'Tab'), 'ABC-123');
});

test('slow typing is a person, not a scan', () => {
  assert.equal(type(createScanDetector(), '8901234567890', 120), null);
});

test('a slow start then a fast burst only reports the burst', () => {
  const d = createScanDetector();
  d.feed('x', 0);
  d.feed('y', 500); // a pause: starts over
  assert.equal(type(d, '12345678', 5, 'Enter', 1000), '12345678');
});

test('too short to be a barcode', () => {
  assert.equal(type(createScanDetector(), '12', 5), null);
});

test('a non-printable key in the middle starts over', () => {
  const d = createScanDetector();
  d.feed('1', 0);
  d.feed('2', 5);
  d.feed('Shift', 10);
  assert.equal(d.feed('Enter', 15), null);
});

test('a configurable gap accepts a slower Bluetooth scanner', () => {
  assert.equal(type(createScanDetector({ maxGap: 60 }), '12345678', 45), '12345678');
  assert.equal(type(createScanDetector({ maxGap: 30 }), '12345678', 45), null);
});

test('findByCode: barcode first, then SKU (case-insensitive)', () => {
  const products = [
    { id: 1, ProductVariants: [{ id: 11, sku: '8901234567890', itemBarcode: null }] },
    { id: 2, ProductVariants: [{ id: 21, sku: 'TEE-M', itemBarcode: '8901234567890' }, { id: 22, sku: 'TEE-L', itemBarcode: '2000000000015' }] },
  ];
  assert.equal(findByCode(products, '8901234567890').variant.id, 21, 'a barcode beats an equal SKU');
  assert.equal(findByCode(products, 'tee-l').variant.id, 22);
  assert.equal(findByCode(products, ' 2000000000015 ').variant.id, 22);
  assert.equal(findByCode(products, 'nope'), null);
  assert.equal(findByCode(products, ''), null);
});
