// Phase 3: the flows added in S5–S9, end to end in the browser.
// Data is made through the API with "E2E-P3 …" names and a unique suffix,
// so runs never collide. Desktop width only (the layout checks at 360 and
// 768 px live in the older specs).
import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { ROLES, go, settle, signIn, watch } from './helpers';

test.skip(!ROLES.owner.email, 'set E2E_OWNER_EMAIL and E2E_OWNER_PASSWORD');

const BASE = 'https://demo-shop.localhost:8443';
const H = { 'X-Requested-With': 'fetch', Origin: BASE };

// api signs in through the API (its own token, not the page's session).
async function api(request, role = 'owner') {
  const { email, password } = ROLES[role];
  const r = await request.post('/api/auth/login', { data: { email, password }, headers: H });
  expect(r.ok(), `API sign-in as ${role}`).toBeTruthy();
  const { token } = await r.json();
  const call = async (method, path, data) => {
    const res = await request.fetch(path, { method, data, headers: { ...H, Authorization: `Bearer ${token}` } });
    const text = await res.text();
    return { status: res.status(), body: text ? JSON.parse(text) : null };
  };
  return {
    get: (p) => call('GET', p),
    post: (p, d) => call('POST', p, d),
    put: (p, d) => call('PUT', p, d),
  };
}

async function product(a, tag, qty = 5) {
  const stores = (await a.get('/api/stores')).body;
  const store = String(stores[0].id);
  const sku = `E2E-P3-${tag}`;
  const r = await a.post('/api/products', {
    name: `E2E-P3 Product ${tag}`, description: 'Phase 3 browser test', productType: 'single', taxType: 'Exclusive', tax: '18',
    sellingType: 'POS', categoryId: '', subCategoryId: '', brandId: '', unitId: '', supplierId: '', slug: '', image: null,
    variants: [{ sku, price: 150, cost: 80, weight: 0, attributes: {}, inventories: [{ storeId: store, qty, quantityAlert: 1 }] }],
    sku, price: '150', quantity: String(qty), storeId: store, quantityAlert: '1',
  });
  expect(r.status, JSON.stringify(r.body)).toBe(201);
  return { id: r.body.id, variantId: r.body.ProductVariants[0].id, sku, store, name: r.body.name };
}

const serious = (r) => r.violations.filter((v) => ['serious', 'critical'].includes(v.impact)).map((v) => `${v.id}: ${v.nodes[0]?.target}`);

test.describe('Phase 3 flows', () => {
  test.skip(({ viewport }) => viewport.width !== 1280, 'desktop only');

  test('new checkout: SKU search, server total, cash payment, sale complete, void from the order page', async ({ page, request }) => {
    test.setTimeout(150_000);
    const log = watch(page);
    const a = await api(request);
    const tag = Date.now().toString(36);
    const p = await product(a, tag);
    const before = (await a.get('/api/settings')).body;
    await a.put('/api/settings', { newCheckout: true });
    try {
      await signIn(page, 'owner');
      await go(page, '/pos');
      await settle(page);
      const search = page.getByRole('searchbox', { name: /Search products/ });
      await expect(search).toBeVisible();
      await search.fill(p.sku);
      await search.press('Enter');
      const cart = page.getByRole('complementary', { name: 'Current sale' });
      await expect(cart.getByText(p.name)).toBeVisible();
      // The server prices the cart: 150 + 18 % = 177.
      const pay = cart.getByRole('button', { name: /PAY/ });
      await expect(pay).toBeEnabled({ timeout: 10_000 });
      await expect(pay).toContainText('177');
      expect(serious(await new AxeBuilder({ page }).include('main').analyze()), 'axe on the checkout').toEqual([]);

      await page.keyboard.press('F10');
      const sheet = page.getByRole('dialog', { name: 'Take payment' });
      await expect(sheet).toBeVisible();
      await sheet.getByRole('button', { name: 'Exact' }).click();
      await sheet.getByRole('button', { name: /Complete sale/ }).click();
      const done = page.getByRole('dialog', { name: /Sale complete/ });
      await expect(done).toBeVisible({ timeout: 15_000 });
      const ref = ((await done.textContent()) || '').match(/[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*-\d+/)?.[0]; // e.g. POS-INV-000037
      expect(ref, 'order number on the success screen').toBeTruthy();
      await done.getByRole('button', { name: /New sale/ }).click();
      await expect(cart.getByText('Cart is empty')).toBeVisible();

      // The order page, then void with a reason.
      await go(page, `/sales?q=${encodeURIComponent(ref)}`, '/sales');
      await settle(page);
      await page.getByRole('link', { name: ref }).first().click();
      await expect(page.getByRole('heading', { name: new RegExp(`Order ${ref}`) })).toBeVisible();
      expect(serious(await new AxeBuilder({ page }).include('main').analyze()), 'axe on the order page').toEqual([]);
      await page.getByRole('button', { name: /Void/ }).click();
      const v = page.getByRole('dialog', { name: /Void order/ });
      await v.getByRole('button', { name: 'Void order' }).click();
      await expect(v.getByRole('alert')).toContainText('reason');
      await v.getByLabel(/Reason/).fill('E2E-P3 test void');
      await v.getByRole('button', { name: 'Void order' }).click();
      await expect(page.getByText(/Voided.*E2E-P3 test void/)).toBeVisible();
      const stock = (await a.get(`/api/products/${p.id}`)).body.ProductVariants[0].Inventories[0].qty;
      expect(stock, 'the void put the item back').toBe(5);
    } finally {
      await a.put('/api/settings', { newCheckout: !!before.newCheckout });
    }
    expect(log.pageErrors).toEqual([]);
  });

  test('purchase page: receive into stock once, record a payment', async ({ page, request }) => {
    test.setTimeout(120_000);
    const a = await api(request);
    const tag = Date.now().toString(36);
    const p = await product(a, `PUR${tag}`, 1);
    const sup = await a.post('/api/suppliers', { name: `E2E-P3 Supplier ${tag}`, status: 'Active' });
    expect(sup.status).toBe(201);
    const pur = await a.post('/api/purchases', {
      reference: `E2E-P3-PUR-${tag}`, date: new Date().toISOString().slice(0, 10), status: 'Ordered', payment_status: 'Unpaid',
      supplierId: String(sup.body.id), purchaseItems: [{ productId: String(p.id), variantId: String(p.variantId), storeId: p.store, quantity: 2, unitPrice: 50 }],
    });
    expect(pur.status, JSON.stringify(pur.body)).toBe(201);

    await signIn(page, 'owner');
    await go(page, `/purchases/${pur.body.id}`);
    await settle(page);
    await expect(page.getByRole('heading', { name: /Purchase E2E-P3-PUR/ })).toBeVisible();
    expect(serious(await new AxeBuilder({ page }).include('main').analyze()), 'axe on the purchase page').toEqual([]);
    await page.getByRole('button', { name: 'Receive into stock' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Receive into stock' }).click();
    await expect(page.getByText(/Received.*the items are in stock/)).toBeVisible();
    await expect(page.getByRole('button', { name: 'Receive into stock' })).toHaveCount(0);
    expect((await a.get(`/api/products/${p.id}`)).body.ProductVariants[0].Inventories[0].qty).toBe(3);

    await page.getByRole('button', { name: 'Record payment' }).click();
    const sheet = page.getByRole('dialog', { name: 'Record payment' });
    await sheet.getByLabel('Amount paid now').fill('9999');
    await sheet.getByRole('button', { name: 'Record payment' }).click();
    await expect(sheet.getByRole('alert')).toContainText('At most');
    await sheet.getByRole('button', { name: /Pay the full/ }).click();
    await sheet.getByRole('button', { name: 'Record payment' }).click();
    await expect(page.getByRole('heading', { name: /Purchase E2E-P3-PUR/ })).toContainText('Paid');
  });

  test('dashboard, stores page and the cashier limits', async ({ page }) => {
    test.setTimeout(120_000);
    await signIn(page, 'owner');
    await go(page, '/dashboard');
    await settle(page);
    await expect(page.getByRole('heading', { name: /Good (morning|afternoon|evening)/ })).toBeVisible();
    await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1); // one main heading (the top bar)
    await expect(page.getByText('Net sales')).toBeVisible();
    await page.getByRole('button', { name: 'Last 7 days' }).click();
    await expect(page).toHaveURL(/range=7d/);
    expect(serious(await new AxeBuilder({ page }).include('main').analyze()), 'axe on the dashboard').toEqual([]);

    await go(page, '/stores');
    await settle(page);
    await expect(page.getByRole('columnheader', { name: 'In stock' })).toBeVisible();
    expect(serious(await new AxeBuilder({ page }).include('main').analyze()), 'axe on stores').toEqual([]);

    test.skip(!ROLES.cashier.email, 'no cashier login');
    await page.context().clearCookies();
    await signIn(page, 'cashier');
    for (const path of ['/users', '/purchases']) {
      await go(page, path);
      await settle(page);
      await expect(page.getByText(/don't have access/), path).toBeVisible();
    }
    // Cashiers hold stores:view: the Stores page is read-only for them.
    await go(page, '/stores');
    await settle(page);
    await expect(page.getByRole('columnheader', { name: 'In stock' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Add store' })).toHaveCount(0);
    await go(page, '/dashboard');
    await settle(page);
    await expect(page.getByText('Your own sales.')).toBeVisible();
    await expect(page.getByText('Due from customers')).toHaveCount(0);
  });
});
