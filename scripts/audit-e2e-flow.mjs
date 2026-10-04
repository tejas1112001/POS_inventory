/**
 * Comprehensive POS Flow Verification & Audit Script
 * Run: node scripts/audit-e2e-flow.mjs
 */
const API = process.env.VITE_API_URL || 'http://localhost:3000';
const PREFIX = 'E2E-AUDIT-' + Math.floor(Math.random() * 10000);

async function request(method, path, body, token) {
  const res = await fetch(`${API}${path}`, {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let data;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = text;
  }
  if (!res.ok) {
    const errorMsg = typeof data === 'object' ? (data?.error || data?.message || JSON.stringify(data)) : data;
    throw new Error(`${method} ${path} → ${res.status}: ${errorMsg}`);
  }
  return data;
}

function findInventory(records, variantId, storeId) {
  const row = records.find((r) => r.variantId === variantId && r.storeId === storeId);
  return row ? row.qty : null;
}

async function main() {
  console.log(`Starting POS E2E Audit with prefix: ${PREFIX}`);

  // 1. Auth
  const auth = await request('POST', '/api/auth/login', {
    email: 'admin@pos.com',
    password: 'admin123',
  });
  const token = auth.token;

  // 2. Master Data Creation
  const category = await request('POST', '/api/categories', { name: `${PREFIX} Category`, status: 'Active' }, token);
  const subCategory = await request('POST', '/api/sub-categories', {
    name: `${PREFIX} SubCategory`,
    status: 'Active',
    categoryId: category.id,
  }, token);
  const brand = await request('POST', '/api/brands', { name: `${PREFIX} Brand`, status: 'Active' }, token);
  const unit = await request('POST', '/api/units', { name: 'Pieces', short_name: 'pcs', status: 'Active' }, token);
  const supplier = await request('POST', '/api/suppliers', {
    name: `${PREFIX} Supplier`,
    email: `supplier-${PREFIX.toLowerCase()}@example.com`,
    phone: '9998887777',
    address: '123 Audit Street',
    status: 'Active',
  }, token);
  const store = await request('POST', '/api/stores', {
    name: `${PREFIX} Store`,
    address: '456 Audit Ave',
    status: 'Active',
  }, token);
  const customer = await request('POST', '/api/customers', {
    name: `${PREFIX} Customer`,
    email: `customer-${PREFIX.toLowerCase()}@example.com`,
    phone: '8887776666',
    status: 'Active',
  }, token);

  console.log('✓ Master data created successfully.');

  // 3. Product with 0 initial stock
  const product = await request('POST', '/api/products', {
    name: `${PREFIX} Product (Zero Stock)`,
    status: 'Active',
    description: 'Testing zero stock product creation',
    categoryId: category.id,
    subCategoryId: subCategory.id,
    brandId: brand.id,
    unitId: unit.id,
    supplierId: supplier.id,
    productType: 'single',
    sellingType: 'POS',
    taxType: 'Exclusive',
    tax: 'GST 5%',
    sku: `${PREFIX}-SKU-ZERO`,
    price: 150,
    cost: 90,
    quantity: 0, // Zero initial stock!
    quantityAlert: 2,
    storeId: store.id,
  }, token);

  const products = await request('GET', '/api/products');
  const fullProduct = products.find((p) => p.id === product.id);
  const variant = fullProduct?.ProductVariants?.[0];
  if (!variant) throw new Error('Product variant not found after creation');

  let inventory = await request('GET', '/api/inventory', null, token);
  let stock0 = findInventory(inventory, variant.id, store.id);
  if (stock0 !== 0) throw new Error(`Expected initial stock 0, got ${stock0}`);
  console.log('✓ Test 1 Passed: Single product successfully created with 0 initial stock.');

  // 4. Purchase in 'Pending' status (stock MUST NOT increment)
  const purchasePending = await request('POST', '/api/purchases', {
    reference: `${PREFIX}-PO-PENDING`,
    date: new Date().toISOString().split('T')[0],
    status: 'Pending', // Pending status!
    payment_status: 'Unpaid',
    total: 900,
    paid: 0,
    due: 900,
    supplierId: supplier.id,
    purchaseItems: [{
      productId: product.id,
      variantId: variant.id,
      storeId: store.id,
      quantity: 10,
      unitPrice: 90,
      subtotal: 900,
    }],
  }, token);

  inventory = await request('GET', '/api/inventory', null, token);
  let stockPending = findInventory(inventory, variant.id, store.id);
  if (stockPending !== 0) throw new Error(`Expected stock 0 for Pending purchase, got ${stockPending}`);
  console.log('✓ Test 2 Passed: Purchase in Pending status did NOT increment stock.');

  // 5. Update purchase status to 'Received' (stock MUST increment by 10)
  await request('PUT', `/api/purchases/${purchasePending.id}`, {
    status: 'Received',
    payment_status: 'Paid',
    paid: 900,
    due: 0,
    storeId: store.id,
  }, token);

  inventory = await request('GET', '/api/inventory', null, token);
  let stockReceived = findInventory(inventory, variant.id, store.id);
  if (stockReceived !== 10) throw new Error(`Expected stock 10 after Received status update, got ${stockReceived}`);
  console.log('✓ Test 3 Passed: Purchase status update to Received successfully incremented stock to 10.');

  // 6. POS Sale beyond stock (15 units when stock is 10) MUST FAIL
  let failedAsExpected = false;
  try {
    await request('POST', '/api/sales', {
      customerId: customer.id,
      storeId: store.id,
      orderItems: [{
        productId: product.id,
        variantId: variant.id,
        quantity: 15,
        unitPrice: 150,
      }],
    }, token);
  } catch (err) {
    failedAsExpected = true;
    console.log(`✓ Test 4 Passed: Overselling blocked by server (${err.message}).`);
  }
  if (!failedAsExpected) throw new Error('Expected sale beyond stock to fail, but it succeeded!');

  // 7. Valid POS Sale (2 units)
  const sale = await request('POST', '/api/sales', {
    customerId: customer.id,
    storeId: store.id,
    tax: 15, // $15 tax
    discount: 10, // $10 discount
    orderItems: [{
      productId: product.id,
      variantId: variant.id,
      quantity: 2,
      unitPrice: 150,
    }],
  }, token);

  if (!sale.reference || !sale.reference.startsWith('POS-INV-')) {
    throw new Error(`Invalid server reference: ${sale.reference}`);
  }
  if (sale.items?.[0]?.qty !== 2 || sale.items?.[0]?.quantity !== 2) {
    throw new Error('Normalized quantity missing on returned sale items');
  }

  inventory = await request('GET', '/api/inventory', null, token);
  let stockAfterSale = findInventory(inventory, variant.id, store.id);
  if (stockAfterSale !== 8) throw new Error(`Expected stock 8 after sale of 2 units, got ${stockAfterSale}`);
  console.log(`✓ Test 5 Passed: Valid POS sale (Reference: ${sale.reference}) succeeded and decremented stock to 8.`);

  // 8. Sales Return Over-Return Attempt (Attempt to return 5 units when 2 were sold) MUST FAIL
  let returnFailedAsExpected = false;
  try {
    await request('POST', '/api/sales/returns', {
      customerId: customer.id,
      storeId: store.id,
      saleId: sale.id,
      returnItems: [{
        productId: product.id,
        variantId: variant.id,
        quantity: 5,
        unitPrice: 150,
      }],
    }, token);
  } catch (err) {
    returnFailedAsExpected = true;
    console.log(`✓ Test 6 Passed: Over-return blocked by server (${err.message}).`);
  }
  if (!returnFailedAsExpected) throw new Error('Expected return exceeding original sale quantity to fail, but it succeeded!');

  // 9. Valid Sales Return (1 unit) linked to original sale
  const salesReturn = await request('POST', '/api/sales/returns', {
    customerId: customer.id,
    storeId: store.id,
    saleId: sale.id,
    status: 'Completed',
    payment_status: 'Paid',
    returnItems: [{
      productId: product.id,
      variantId: variant.id,
      quantity: 1,
      unitPrice: 150,
      reason: 'Defective item',
    }],
  }, token);

  inventory = await request('GET', '/api/inventory', null, token);
  let stockAfterReturn = findInventory(inventory, variant.id, store.id);
  if (stockAfterReturn !== 9) throw new Error(`Expected stock 9 after valid return of 1 unit, got ${stockAfterReturn}`);
  console.log(`✓ Test 7 Passed: Linked Sales Return (ID: ${salesReturn.id}) succeeded and restored stock to 9.`);

  console.log('\n======================================================');
  console.log('🎉 ALL 7 E2E POS FLOW VERIFICATION CHECKS PASSED!');
  console.log('======================================================\n');
}

main().catch((err) => {
  console.error('\n❌ POS E2E AUDIT TEST FAILED:');
  console.error(err.message);
  process.exit(1);
});
