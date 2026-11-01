// S1: the forms that could not save, and the other S1 fixes, as the owner.
// (Customer, supplier and brand forms were rebuilt in S4: see s4-kit.spec.js.)
// Records are named "E2E-S1 … w<width> <time>" so every run is unique.
import { test, expect } from '@playwright/test';
import { ROLES, go, settle, shot, signIn, watch } from './helpers';

test.skip(!ROLES.owner.email, 'set E2E_OWNER_EMAIL and E2E_OWNER_PASSWORD');

const next = (page) => page.getByRole('button', { name: 'Next', exact: true }).click();

async function pick(select, text) {
  const options = await select.locator('option').allTextContents();
  const label = options.find((o) => o.includes(text));
  expect(label, `option "${text}"`).toBeTruthy();
  await select.selectOption({ label });
}

test('S1 forms and fixes', async ({ page }, info) => {
  test.setTimeout(240_000);
  const log = watch(page);
  const tag = `${info.project.name} ${Date.now().toString(36)}`;
  await signIn(page, 'owner');

  await test.step('Add Product: single product with 18 % tax saves from the last step', async () => {
    await go(page, '/add-product');
    await settle(page);
    await page.locator('select[name=storeId]').selectOption({ index: 1 });
    await page.fill('input[name=name]', `E2E-S1 Product ${tag}`);
    await page.fill('input[name=sku]', `E2E-S1-${tag.replace(/\W/g, '')}`);
    await page.locator('select[name=sellingType]').selectOption('POS');
    await pick(page.locator('select[name=categoryId]'), 'UI-AUDIT Category');
    await next(page);
    await page.fill('input[name=quantity]', '3');
    await page.fill('input[name=price]', '99.5');
    await page.fill('input[name=tax]', '18');
    await next(page);
    await next(page);
    await page.getByRole('button', { name: 'Add Product' }).click();
    await page.waitForURL('**/products');
    await page.fill('input[placeholder*="earch"]', `E2E-S1 Product ${tag}`);
    await expect(page.getByText(`E2E-S1 Product ${tag}`).first()).toBeVisible();
    await shot(page, `s1-${info.project.name}-product-saved`);
  });

  await test.step('Edit Product: a product without a brand saves', async () => {
    await page.locator(`tr:has-text("E2E-S1 Product ${tag}") button[aria-label="Edit"]`).click();
    await page.locator('textarea[name=description]').fill('Edited by the S1 test');
    await page.getByRole('button', { name: 'Save Product' }).click();
    await expect(page.getByRole('button', { name: 'Save Product' })).toBeHidden();
  });

  await test.step('Add Purchase: sizes are listed and the line total updates', async () => {
    await go(page, '/add-purchase');
    await settle(page);
    await page.fill('input[name=reference]', `E2E-S1-PUR ${tag}`);
    await pick(page.locator('select[name=supplierId]'), 'UI-AUDIT Supplier');
    await next(page);
    await page.getByRole('button', { name: 'Add Item' }).click();
    await pick(page.locator('tbody select').first(), 'UI-AUDIT T-Shirt');
    const variants = await page.locator('tbody select').nth(1).locator('option').count();
    expect(variants, 'size options').toBeGreaterThan(2);
    await page.fill('input[name=quantity_0]', '2');
    await page.fill('input[name=unitPrice_0]', '250');
    await expect(page.locator('tbody input[readonly]').first()).toHaveValue('500.00');
    await shot(page, `s1-${info.project.name}-purchase-line`);
    await page.getByRole('button', { name: 'Add Purchase' }).click();
    await page.waitForURL('**/purchases');
  });

  await test.step('POS: the store picker shows the store', async () => {
    await go(page, '/pos');
    await settle(page);
    await expect(page.getByRole('combobox').first()).toContainText('Main Store');
  });

  await test.step('Showcase: changing the web address asks first (cancel keeps it)', async () => {
    await go(page, '/showcase-settings');
    await settle(page);
    const slug = page.locator('input[placeholder="e.g. abc-fashion"]');
    const before = await slug.inputValue();
    await slug.fill(`${before}-e2e`);
    await page.getByRole('button', { name: 'Save Profile Changes' }).click();
    // Since S2 this is an app dialog (was a browser pop-up in S1).
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('web address');
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await slug.fill(before);
  });

  await test.step('Sales Report shows product SKUs', async () => {
    await go(page, '/sales-report');
    await settle(page);
    const skus = await page.locator('tbody tr td:nth-child(4)').allTextContents();
    expect(skus.length).toBeGreaterThan(0);
    expect(skus.some((s) => /^SO-\d+-\d+$/.test(s.trim())), skus.join(',')).toBe(false);
  });

  await test.step('Category pictures load (no 401 for files)', async () => {
    const before = log.consoleErrors.length;
    await go(page, '/categories');
    await settle(page);
    const fileErrors = log.consoleErrors.slice(before).filter((e) => e.includes('401'));
    expect(fileErrors).toEqual([]);
  });

  expect(log.pageErrors).toEqual([]);
});
