/**
 * End-to-end flow test: master data → product → purchase → POS sale → inventory verify
 * Run: node scripts/e2e-flow-test.mjs
 */
const API = process.env.VITE_API_URL || 'http://localhost:3000';
const PREFIX = 'FLOW-TEST';

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
    throw new Error(`${method} ${path} → ${res.status}: ${typeof data === 'object' ? JSON.stringify(data) : data}`);
  }
  return data;
}

function findInventory(records, variantId, storeId) {
  const row = records.find((r) => r.variantId === variantId && r.storeId === storeId);
  return row ? row.qty : null;
}

async function main() {
  const results = {
    prefix: PREFIX,
    timestamp: new Date().toISOString(),
    masterData: {},
    product: {},
    purchase: {},
    sale: {},
    inventory: {},
    verification: { passed: false, checks: [] },
  };

  // ── Auth ──
  const auth = await request('POST', '/api/auth/login', {
    email: 'admin@pos.com',
    password: 'admin123',
  });
  const token = auth.token;
  if (auth.user.role !== 'admin') {
    throw new Error(`Expected admin role, got ${auth.user.role}`);
  }

  // ── Master data ──
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
    email: 'flowtest-supplier@example.com',
    phone: '9999999999',
    address: '123 Test Street',
    status: 'Active',
  }, token);
  const store = await request('POST', '/api/stores', {
    name: `${PREFIX} Store`,
    address: '456 Test Avenue',
    status: 'Active',
  }, token);
  const customer = await request('POST', '/api/customers', {
    name: `${PREFIX} Customer`,
    email: 'flowtest-customer@example.com',
    phone: '8888888888',
    address: '789 Customer Lane',
    city: 'Test City',
    country: 'Test Country',
    status: 'Active',
  }, token);

  results.masterData = {
    category: { id: category.id, name: category.name },
    subCategory: { id: subCategory.id, name: subCategory.name },
    brand: { id: brand.id, name: brand.name },
    unit: { id: unit.id, name: unit.name, shortName: unit.short_name },
    supplier: { id: supplier.id, name: supplier.name },
    store: { id: store.id, name: store.name },
    customer: { id: customer.id, name: customer.name },
  };

  // ── Product (initial stock = 1, minimum allowed by API) ──
  const PRODUCT_PRICE = 100;
  const PRODUCT_COST = 60;
  const INITIAL_QTY = 1;
  const PURCHASE_QTY = 10;
  const PURCHASE_UNIT_PRICE = 60;
  const SALE_QTY = 1;

  const product = await request('POST', '/api/products', {
    name: `${PREFIX} Widget`,
    status: 'Active',
    description: 'End-to-end flow test product',
    categoryId: category.id,
    subCategoryId: subCategory.id,
    brandId: brand.id,
    unitId: unit.id,
    supplierId: supplier.id,
    productType: 'single',
    sellingType: 'POS',
    taxType: 'Exclusive',
    tax: 'GST 5%',
    sku: `${PREFIX}-SKU-001`,
    itemBarcode: `${PREFIX}-BAR-001`,
    price: PRODUCT_PRICE,
    cost: PRODUCT_COST,
    quantity: INITIAL_QTY,
    quantityAlert: 2,
    storeId: store.id,
  }, token);

  const products = await request('GET', '/api/products');
  const fullProduct = products.find((p) => p.id === product.id);
  const variant = fullProduct?.ProductVariants?.[0];
  if (!variant) throw new Error('Product variant not found after creation');

  results.product = {
    id: product.id,
    name: product.name,
    sku: variant.sku,
    variantId: variant.id,
    price: PRODUCT_PRICE,
    cost: PRODUCT_COST,
    storeId: store.id,
    initialStock: INITIAL_QTY,
  };

  let inventory = await request('GET', '/api/inventory', null, token);
  let stockAfterCreate = findInventory(inventory, variant.id, store.id);
  results.inventory.afterProductCreate = stockAfterCreate;

  // ── Purchase ──
  const purchaseTotal = PURCHASE_QTY * PURCHASE_UNIT_PRICE;
  const purchase = await request('POST', '/api/purchases', {
    reference: `${PREFIX}-PO-001`,
    date: new Date().toISOString().split('T')[0],
    status: 'Received',
    payment_status: 'Paid',
    total: purchaseTotal,
    paid: purchaseTotal,
    due: 0,
    supplierId: supplier.id,
    purchaseItems: [{
      productId: product.id,
      variantId: variant.id,
      storeId: store.id,
      quantity: PURCHASE_QTY,
      unitPrice: PURCHASE_UNIT_PRICE,
      discount: 0,
      subtotal: purchaseTotal,
    }],
  }, token);

  inventory = await request('GET', '/api/inventory', null, token);
  const stockAfterPurchase = findInventory(inventory, variant.id, store.id);
  const expectedAfterPurchase = INITIAL_QTY + PURCHASE_QTY;
  results.purchase = {
    id: purchase.id,
    reference: purchase.reference,
    quantity: PURCHASE_QTY,
    unitPrice: PURCHASE_UNIT_PRICE,
    total: purchaseTotal,
    supplier: supplier.name,
  };
  results.inventory.afterPurchase = stockAfterPurchase;
  results.inventory.expectedAfterPurchase = expectedAfterPurchase;

  // ── POS sale (1 unit) ──
  const saleSubtotal = PRODUCT_PRICE * SALE_QTY;
  const sale = await request('POST', '/api/sales', {
    reference: `${PREFIX}-POS-001`,
    date: new Date().toISOString(),
    status: 'Completed',
    payment_status: 'Paid',
    payment_method: 'cash',
    subtotal: saleSubtotal,
    discount: 0,
    tax: 0,
    shipping: 0,
    total: saleSubtotal,
    paid: saleSubtotal,
    due: 0,
    customerId: customer.id,
    storeId: store.id,
    orderItems: [{
      productId: product.id,
      variantId: variant.id,
      quantity: SALE_QTY,
      unitPrice: PRODUCT_PRICE,
      discount: 0,
      total: saleSubtotal,
    }],
  }, token);

  inventory = await request('GET', '/api/inventory', null, token);
  const stockAfterSale = findInventory(inventory, variant.id, store.id);
  const expectedAfterSale = expectedAfterPurchase - SALE_QTY;
  results.sale = {
    id: sale.id,
    reference: sale.reference,
    quantity: SALE_QTY,
    unitPrice: PRODUCT_PRICE,
    subtotal: saleSubtotal,
    total: sale.total,
    customer: customer.name,
  };
  results.inventory.afterSale = stockAfterSale;
  results.inventory.expectedAfterSale = expectedAfterSale;

  // ── Verification ──
  const checks = [
    {
      name: 'Initial stock after product create',
      expected: INITIAL_QTY,
      actual: stockAfterCreate,
      pass: stockAfterCreate === INITIAL_QTY,
    },
    {
      name: 'Stock increased after purchase',
      expected: expectedAfterPurchase,
      actual: stockAfterPurchase,
      pass: stockAfterPurchase === expectedAfterPurchase,
    },
    {
      name: 'Stock decreased by 1 after POS sale',
      expected: expectedAfterSale,
      actual: stockAfterSale,
      pass: stockAfterSale === expectedAfterSale,
    },
    {
      name: 'Purchase total correct',
      expected: purchaseTotal,
      actual: parseFloat(purchase.total),
      pass: parseFloat(purchase.total) === purchaseTotal,
    },
    {
      name: 'Sale total correct',
      expected: saleSubtotal,
      actual: parseFloat(sale.total),
      pass: parseFloat(sale.total) === saleSubtotal,
    },
  ];

  const purchases = await request('GET', '/api/purchases');
  const savedPurchase = purchases.find((p) => p.id === purchase.id);
  checks.push({
    name: 'Purchase retrievable via GET /api/purchases',
    expected: purchase.id,
    actual: savedPurchase?.id,
    pass: savedPurchase?.id === purchase.id,
  });

  const sales = await request('GET', '/api/sales', null, token);
  const savedSale = sales.find((s) => s.id === sale.id);
  checks.push({
    name: 'Sale retrievable via GET /api/sales',
    expected: sale.id,
    actual: savedSale?.id,
    pass: savedSale?.id === sale.id,
  });
  checks.push({
    name: 'Sale order item quantity',
    expected: SALE_QTY,
    actual: savedSale?.items?.[0]?.quantity,
    pass: savedSale?.items?.[0]?.quantity === SALE_QTY,
  });

  results.verification.checks = checks;
  results.verification.passed = checks.every((c) => c.pass);

  console.log(JSON.stringify(results, null, 2));
  if (!results.verification.passed) {
    process.exit(1);
  }
}

main().catch((err) => {
  console.error(err.message);
  process.exit(1);
});
