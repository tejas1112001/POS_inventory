// Unit tests for the sale units helpers. Run with `npm run test:unit`.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { lineBase, lineQtyText, maxInUnit, purchaseUnitsOf, saleUnitsOf, unitOptionLabel, unitQtyText } from './saleUnits.js';

test('sale units only, as numbers', () => {
  const p = { units: [{ unitId: 3, name: 'Box', factor: '12', forSale: true }, { unitId: 4, name: 'Carton', factor: 144, forSale: false }, { unitId: 5, name: 'Pack', factor: 6 }] };
  assert.deepEqual(saleUnitsOf(p), [{ unitId: 3, name: 'Box', factor: 12 }, { unitId: 5, name: 'Pack', factor: 6 }]);
  assert.deepEqual(saleUnitsOf({}), []);
  assert.deepEqual(saleUnitsOf(null), []);
  assert.deepEqual(purchaseUnitsOf({ units: [{ unitId: 3, name: 'Box', factor: 12, forPurchase: false }, { unitId: 4, name: 'Carton', factor: '144', forSale: false }] }),
    [{ unitId: 4, name: 'Carton', factor: 144 }]);
});

test('base quantity of a line', () => {
  assert.equal(lineBase({ quantity: 2, factor: 12 }), 24);
  assert.equal(lineBase({ quantity: 3 }), 3);
  assert.equal(lineBase({ quantity: 0.1, factor: 3 }), 0.3, 'no float noise');
});

test('how many of a unit fit the stock', () => {
  assert.equal(maxInUnit(76, 12), 6);
  assert.equal(maxInUnit(12, 12), 1);
  assert.equal(maxInUnit(11, 12), 0);
  assert.equal(maxInUnit(10, 1), 10);
  assert.equal(maxInUnit(2.5, 0.5, true), 5);
  assert.equal(maxInUnit(1, 3, true), 0.333);
  assert.equal(maxInUnit(0, 12), 0);
  assert.equal(maxInUnit(-3, 12), 0);
});

test('labels', () => {
  assert.equal(unitOptionLabel({ name: 'Box', factor: 12 }), 'Box (12)');
  assert.equal(lineQtyText({ quantity: 2, unitId: 3, unitName: 'Box', factor: 12 }), '2 Box (24)');
  assert.equal(lineQtyText({ quantity: 1.25 }), '1.25');
});

test('saved lines show their unit', () => {
  assert.equal(unitQtyText({ quantity: 24, unitName: 'Box', unitQty: 2, unitFactor: 12 }), '2 Box (24)');
  assert.equal(unitQtyText({ quantity: '1.500', unitName: null, unitQty: null }), '1.5');
  assert.equal(unitQtyText({ soldQty: 48, unitName: 'Carton', unitQty: 2 }, 48), '2 Carton (48)');
  assert.equal(unitQtyText(null, 3), '3');
});
