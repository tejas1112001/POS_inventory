/**
 * Concurrent POS Checkout & Database Concurrency Benchmark Script
 * Run: node scripts/concurrent-checkout-test.mjs
 */
const API = process.env.VITE_API_URL || 'http://localhost:3000';
const CONCURRENT_REGISTERS = 10;
const QTY_PER_SALE = 1;

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

async function main() {
  console.log(`Starting Concurrent POS Checkout Test (${CONCURRENT_REGISTERS} concurrent checkout requests)...`);

  // Auth
  const auth = await request('POST', '/api/auth/login', {
    email: 'admin@pos.com',
    password: 'admin123',
  });
  const token = auth.token;

  const prefix = 'CONCURRENCY-' + Math.floor(Math.random() * 10000);
  const category = await request('POST', '/api/categories', { name: `${prefix} Category`, status: 'Active' }, token);
  const supplier = await request('POST', '/api/suppliers', { name: `${prefix} Supplier`, status: 'Active' }, token);
  const store = await request('POST', '/api/stores', { name: `${prefix} Store`, status: 'Active' }, token);
  const customer = await request('POST', '/api/customers', { name: `${prefix} Customer`, status: 'Active' }, token);

  // Create product with 20 units stock
  const INITIAL_STOCK = 20;
  const product = await request('POST', '/api/products', {
    name: `${prefix} Widget`,
    status: 'Active',
    categoryId: category.id,
    supplierId: supplier.id,
    productType: 'single',
    sellingType: 'POS',
    sku: `${prefix}-SKU`,
    price: 100,
    cost: 50,
    quantity: INITIAL_STOCK,
    storeId: store.id,
  }, token);

  const products = await request('GET', '/api/products');
  const fullProduct = products.find((p) => p.id === product.id);
  const variantId = fullProduct?.ProductVariants?.[0]?.id;

  console.log(`Product created with Initial Stock = ${INITIAL_STOCK}. Firing ${CONCURRENT_REGISTERS} parallel sales...`);

  const startTime = Date.now();
  const checkoutPromises = Array.from({ length: CONCURRENT_REGISTERS }).map((_, idx) =>
    request('POST', '/api/sales', {
      customerId: customer.id,
      storeId: store.id,
      orderItems: [{
        productId: product.id,
        variantId: variantId,
        quantity: QTY_PER_SALE,
        unitPrice: 100,
      }],
    }, token)
  );

  const results = await Promise.allSettled(checkoutPromises);
  const duration = Date.now() - startTime;

  const fulfilled = results.filter((r) => r.status === 'fulfilled');
  const rejected = results.filter((r) => r.status === 'rejected');

  console.log(`\nConcurrency Test Completed in ${duration} ms:`);
  console.log(`✓ Successful Checkout Requests: ${fulfilled.length}`);
  console.log(`✗ Rejected Checkout Requests: ${rejected.length}`);

  // Verify stock deduction
  const inventory = await request('GET', '/api/inventory', null, token);
  const finalStock = inventory.find((r) => r.variantId === variantId && r.storeId === store.id)?.qty;
  const expectedStock = INITIAL_STOCK - (fulfilled.length * QTY_PER_SALE);

  console.log(`Initial Stock: ${INITIAL_STOCK}`);
  console.log(`Expected Final Stock: ${expectedStock}`);
  console.log(`Actual Final Stock: ${finalStock}`);

  if (finalStock !== expectedStock) {
    throw new Error(`Stock mismatch after concurrent checkouts! Expected ${expectedStock}, got ${finalStock}`);
  }

  console.log('🎉 CONCURRENCY TEST PASSED! Inventory remains 100% mathematically consistent under parallel checkouts.');
}

main().catch((err) => {
  console.error('❌ CONCURRENCY TEST FAILED:', err.message);
  process.exit(1);
});
