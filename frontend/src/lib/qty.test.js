// Unit tests for the quantity helpers. Run with `npm run test:unit`.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { addQty, fmtQty, parseQty, qtyStep, roundQty } from './qty.js';

test('float noise is dropped at three decimals', () => {
  assert.equal(roundQty(0.1 + 0.2), 0.3);
  assert.equal(addQty(0.1, 0.2), 0.3);
  assert.equal(addQty(1.25, -0.25), 1);
  assert.equal(roundQty('x'), 0);
});

test('whole-unit products take whole numbers only', () => {
  assert.equal(parseQty('3'), 3);
  assert.equal(parseQty(' 12 '), 12);
  assert.equal(parseQty('1.5'), null);
  assert.equal(parseQty('2.000'), 2, 'trailing zeros are whole');
  assert.equal(parseQty('-1'), null);
  assert.equal(parseQty(''), null);
  assert.equal(parseQty('abc'), null);
});

test('fractional products take up to three decimals', () => {
  const f = { fractional: true };
  assert.equal(parseQty('1.25', f), 1.25);
  assert.equal(parseQty('0.005', f), 0.005);
  assert.equal(parseQty('.5', f), 0.5);
  assert.equal(parseQty('1.2345', f), null);
  assert.equal(parseQty('1.2500', f), 1.25, 'extra zeros are fine');
  assert.equal(parseQty('.', f), null);
});

test('steps and display', () => {
  assert.equal(qtyStep(true), '0.001');
  assert.equal(qtyStep(false), '1');
  assert.equal(fmtQty(2), '2');
  assert.equal(fmtQty(1.25), '1.25');
  assert.equal(fmtQty(1234.5), '1,234.5');
});
