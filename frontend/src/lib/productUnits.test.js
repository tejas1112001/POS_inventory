// Unit tests for the product units helpers. Run with `npm run test:unit`.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { MAX_UNITS, emptyUnitRow, unitRowsFromProduct, unitsPayload, unitsProblem } from './productUnits.js';

const row = (unitId, factor, more = {}) => ({ ...emptyUnitRow(), unitId, factor, ...more });

test('rows come from a loaded product', () => {
  assert.deepEqual(unitRowsFromProduct({ units: [{ unitId: 7, factor: 12, forSale: true, forPurchase: false }] }),
    [{ unitId: '7', factor: '12', forSale: true, forPurchase: false }]);
  assert.deepEqual(unitRowsFromProduct({}), []);
  assert.deepEqual(unitRowsFromProduct(null), []);
});

test('blank rows are ignored and need nothing', () => {
  assert.equal(unitsProblem([emptyUnitRow()], '', false), '');
  assert.deepEqual(unitsPayload([emptyUnitRow(), row('7', '12')]), [{ unitId: 7, factor: 12, forSale: true, forPurchase: true }]);
});

test('the rules the server checks', () => {
  assert.match(unitsProblem([row('7', '12')], '', false), /product's unit first/);
  assert.match(unitsProblem([row('', '12')], '1', false), /choose a unit/);
  assert.match(unitsProblem([row('1', '12')], '1', false), /already the product's unit/);
  assert.match(unitsProblem([row('7', '12'), row('7', '6')], '1', false), /listed twice/);
  assert.match(unitsProblem([row('7', '0')], '1', false), /above 0/);
  assert.match(unitsProblem([row('7', '1.2345')], '1', true), /at most 3 decimals/);
  assert.match(unitsProblem([row('7', '0.5')], '1', false), /whole number/);
  assert.equal(unitsProblem([row('7', '0.5')], '1', true), '', 'half a kg of a product kept in kg');
  assert.match(unitsProblem([row('7', '12', { forSale: false, forPurchase: false })], '1', false), /sale, purchase or both/);
  const many = Array.from({ length: MAX_UNITS + 1 }, (_, i) => row(String(i + 10), '2'));
  assert.match(unitsProblem(many, '1', false), /at most 10/);
  assert.equal(unitsProblem([row('7', '12'), row('8', '144', { forSale: false })], '1', false), '');
});

test('the payload sends numbers', () => {
  assert.deepEqual(unitsPayload([row('8', '0.25', { forPurchase: false })]), [{ unitId: 8, factor: 0.25, forSale: true, forPurchase: false }]);
});
