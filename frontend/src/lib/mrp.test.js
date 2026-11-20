// Unit tests for the MRP warning helpers. Run with `npm run test:unit`.
// The rule matches the server's (pricing.AboveMRP).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { aboveMrp, mrpBody, mrpWarning, taxInclusivePrice } from './mrp.js';

test('exclusive prices are compared with their tax added', () => {
  assert.equal(taxInclusivePrice(100, 18, 'Exclusive'), 118);
  assert.equal(aboveMrp({ price: 100, mrp: 118, taxRate: 18, taxType: 'Exclusive' }), false, 'at MRP');
  assert.equal(aboveMrp({ price: 100.01, mrp: 118, taxRate: 18, taxType: 'Exclusive' }), true);
});

test('inclusive prices are compared as they are', () => {
  assert.equal(aboveMrp({ price: 118, mrp: 118, taxRate: 18, taxType: 'Inclusive' }), false);
  assert.equal(aboveMrp({ price: 118.01, mrp: 118, taxRate: 18, taxType: 'Inclusive' }), true);
});

test('without a tax type the shop default decides', () => {
  assert.equal(aboveMrp({ price: 110, mrp: 118, taxRate: 18, taxType: '', pricesIncludeTax: true }), false);
  assert.equal(aboveMrp({ price: 110, mrp: 118, taxRate: 18, taxType: '', pricesIncludeTax: false }), true);
});

test('no MRP or no price is never above', () => {
  assert.equal(aboveMrp({ price: 500, mrp: '' }), false);
  assert.equal(aboveMrp({ price: '', mrp: 10 }), false);
  assert.equal(mrpWarning({ price: 5, mrp: 10 }), '');
  assert.match(mrpWarning({ price: 20, mrp: 10 }), /above the MRP \(10\.00\)/);
});

test('mrpBody sends a number or null', () => {
  assert.equal(mrpBody(''), null);
  assert.equal(mrpBody('45.5'), 45.5);
  assert.equal(mrpBody('abc'), null);
});
