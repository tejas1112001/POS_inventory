// Unit tests for the receipt's GST block. Run with `npm run test:unit`.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { receiptGst } from './gstReceipt.js';

const states = [{ code: '27', name: 'Maharashtra' }, { code: '29', name: 'Karnataka' }, { code: '04', name: 'Chandigarh' }];

test('a B2B sale prints its own copy', () => {
  const sale = {
    gst: {
      sellerGstin: '29AAGCB7383J1Z4', sellerState: '29', buyerGstin: '27AAPFU0939F1ZV', buyerName: 'Kaveri Traders Pvt Ltd',
      buyerAddress: '4 Residency Road', buyerState: '27', placeOfSupply: '27', taxSplit: 'igst', invoiceKind: 'b2b',
    },
  };
  const r = receiptGst(sale, { taxRegistrationNo: '27AAPFU0939F1ZV' }, 'GST', states);
  assert.equal(r.title, 'Tax Invoice');
  assert.equal(r.taxId, 'GSTIN: 29AAGCB7383J1Z4', "the store's GSTIN, not the shop's");
  assert.equal(r.placeOfSupply, 'Maharashtra (27)');
  assert.deepEqual(r.billTo, ['Kaveri Traders Pvt Ltd', 'GSTIN: 27AAPFU0939F1ZV', '4 Residency Road', 'State: Maharashtra (27)']);
});

test('UTGST, and no tax invoice without a GSTIN', () => {
  const r = receiptGst({ gst: { sellerState: '04', placeOfSupply: '04', taxSplit: 'cgst_utgst', invoiceKind: 'b2c' } }, {}, 'GST', states);
  assert.equal(r.title, 'Receipt');
  assert.equal(r.secondHalf, 'UTGST');
  assert.deepEqual(r.billTo, []);
});

test('before M3 or with no state: the shop settings', () => {
  assert.equal(receiptGst({ gst: null }, { taxRegistrationNo: 'VAT-1' }, 'VAT', states).taxId, 'VAT No: VAT-1');
  assert.equal(receiptGst({ gst: { taxSplit: 'igst', sellerState: null } }, { taxRegistrationNo: '27AAPFU0939F1ZV' }, 'GST', states).taxId, 'GSTIN: 27AAPFU0939F1ZV');
  assert.equal(receiptGst({}, {}, 'GST', states).title, 'Receipt');
});
