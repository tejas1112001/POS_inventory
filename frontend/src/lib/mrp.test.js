// Unit tests for the MRP warning helpers. Run with `npm run test:unit`.
// The rule matches the server's (pricing.AboveMRP, pharmacy plan §6.2).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { aboveMrp, mrpBody, mrpPackBody, mrpText, mrpWarning, taxInclusivePrice, unitShare } from './mrp.js';

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

test('an MRP printed per pack: the pack and one loose unit are both checked', () => {
  assert.equal(aboveMrp({ price: 3.5, mrp: 35, mrpPack: 10, taxType: 'Inclusive' }), false, 'strip at MRP');
  assert.equal(aboveMrp({ price: 3.51, mrp: 35, mrpPack: 10, taxType: 'Inclusive' }), true, 'strip ₹35.10');
  assert.equal(unitShare(25, 15), 1.66, '₹1.666… rounded down');
  assert.equal(unitShare(35, 10), 3.5);
  assert.equal(aboveMrp({ price: 1.66, mrp: 25, mrpPack: 15, taxType: 'Inclusive' }), false);
  assert.equal(aboveMrp({ price: 1.6667, mrp: 25, mrpPack: 15, taxType: 'Inclusive' }), true, 'one tablet would be ₹1.67');
  assert.equal(aboveMrp({ price: 33, mrp: 35, taxRate: 12, taxType: 'Exclusive' }), true, '₹36.96 with tax');
  assert.equal(aboveMrp({ price: 3.5, mrp: 35, mrpPack: '', taxType: 'Inclusive' }), false, 'no pack: one unit');
});

test('mrpText and mrpPackBody', () => {
  assert.equal(mrpText(35, 10), '35.00 for 10');
  assert.equal(mrpText(35, 1), '35.00');
  assert.equal(mrpText(null, 10), '');
  assert.equal(mrpPackBody(''), undefined);
  assert.equal(mrpPackBody('0'), undefined);
  assert.equal(mrpPackBody('15'), 15);
  assert.match(mrpWarning({ price: 3.6, mrp: 35, mrpPack: 10, taxType: 'Inclusive' }), /35\.00 for 10/);
});
