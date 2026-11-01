// S4: the component kit on the six pilot pages (categories, brands, units,
// customers, suppliers, stock): server search and filters, cards on phones,
// add/edit in a sheet with field errors, confirm on delete (cancelled:
// nothing is deleted), accessibility of the page area.
import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { ROLES, go, settle, shot, signIn, watch } from './helpers';

const PAGES = [
  { path: '/categories', add: '/add-category', noun: 'Category', label: 'Category name', seed: 'UI-AUDIT Category' },
  { path: '/brands', add: '/add-brand', noun: 'Brand', label: 'Brand name', seed: 'UI-AUDIT Brand' },
  { path: '/units', add: '/add-unit', noun: 'Unit', label: 'Unit name', seed: 'UI-AUDIT Unit' },
  { path: '/customers', add: '/add-customer', noun: 'Customer', label: 'Name', seed: 'UI-AUDIT Customer' },
  { path: '/suppliers', add: '/add-supplier', noun: 'Supplier', label: 'Name', seed: 'UI-AUDIT Supplier' },
];

const seriousAxe = async (page, selector) =>
  (await new AxeBuilder({ page }).include(selector).analyze()).violations
    .filter((v) => ['serious', 'critical'].includes(v.impact))
    .map((v) => `${v.id}: ${v.nodes.map((n) => `${n.target.join(' ')} (${(n.any[0] || n.all[0] || {}).message || ''})`).join('; ')}`);

async function visibleRowsText(page) {
  const width = page.viewportSize().width;
  const sel = width < 640 ? 'main ul[aria-label] > li' : 'main table tbody tr';
  return (await page.locator(sel).allInnerTexts()).join('\n');
}

test('owner: pilot pages', async ({ page }, info) => {
  test.skip(!ROLES.owner.email, 'set E2E_OWNER_EMAIL and E2E_OWNER_PASSWORD');
  test.setTimeout(600_000);
  const log = watch(page);
  const width = page.viewportSize().width;
  const run = `${info.project.name} ${Date.now().toString(36)}`;
  await signIn(page, 'owner');

  for (const p of PAGES) {
    await test.step(`${p.noun}: list, search, status, cards`, async () => {
      await go(page, p.path);
      await settle(page);
      const search = page.getByRole('searchbox').first();
      await search.fill(p.seed);
      await expect.poll(() => visibleRowsText(page)).toContain(p.seed);
      expect(new URL(page.url()).searchParams.get('q')).toBe(p.seed); // kept in the address
      if (width < 640) {
        await expect(page.locator('main table')).toBeHidden();
        await page.getByRole('button', { name: /^Filters/ }).click();
        await page.getByRole('dialog').getByLabel('Status').selectOption('Inactive');
        await page.getByRole('button', { name: 'Show results' }).click();
      } else {
        await page.getByLabel('Status').selectOption('Inactive');
      }
      await expect(page.getByRole('button', { name: /Remove filter Status/ })).toBeVisible();
      await expect.poll(() => visibleRowsText(page)).not.toContain(p.seed); // the seed record is Active
      await page.getByRole('button', { name: 'Clear filters' }).first().click();
      await shot(page, `s4-${info.project.name}-${p.noun.toLowerCase()}-list`);
      expect(await seriousAxe(page, 'main')).toEqual([]);
    });

    await test.step(`${p.noun}: add with a field error, then edit`, async () => {
      await page.getByRole('button', { name: `Add ${p.noun.toLowerCase()}` }).first().click();
      const sheet = page.getByRole('dialog');
      await sheet.getByRole('button', { name: `Add ${p.noun.toLowerCase()}` }).click();
      const nameBox = sheet.getByLabel(new RegExp(`^${p.label}`));
      await expect(nameBox).toHaveAttribute('aria-invalid', 'true');
      await expect(sheet.getByText(`${p.label} is required.`)).toBeVisible();
      await shot(page, `s4-${info.project.name}-${p.noun.toLowerCase()}-sheet-error`);
      expect(await seriousAxe(page, '[role=dialog]')).toEqual([]);
      const name = `E2E-S4 ${p.noun} ${run}`;
      await nameBox.fill(name);
      if (p.noun === 'Unit') await sheet.getByLabel('Short name').fill('e2e');
      await sheet.getByRole('button', { name: `Add ${p.noun.toLowerCase()}` }).click();
      await expect(page.locator('.toast--success', { hasText: `${p.noun} added.` })).toBeVisible();
      await page.getByRole('searchbox').first().fill(name);
      await expect.poll(() => visibleRowsText(page)).toContain(name);
      await page.getByRole('button', { name: `Actions for ${name}` }).click();
      await page.getByRole('menuitem', { name: 'Edit' }).click();
      await page.getByRole('dialog').getByLabel(new RegExp(`^${p.label}`)).fill(`${name} edited`);
      await page.getByRole('dialog').getByRole('button', { name: 'Save changes' }).click();
      await expect(page.locator('.toast--success', { hasText: `${p.noun} updated.` })).toBeVisible();
      await page.getByRole('button', { name: `Actions for ${name} edited` }).click();
      await page.getByRole('menuitem', { name: 'Delete' }).click();
      await expect(page.getByRole('dialog')).toContainText(`Delete ${p.noun.toLowerCase()}`);
      await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
      await expect.poll(() => visibleRowsText(page)).toContain(`${name} edited`);
    });

    await test.step(`${p.noun}: the add address opens the sheet`, async () => {
      await go(page, p.add);
      await expect(page.getByRole('dialog')).toBeVisible();
      await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
      await page.waitForURL(`**${p.path}`);
    });
  }

  await test.step('Stock: counters, filters, set-stock validation', async () => {
    await go(page, '/inventory');
    await settle(page);
    await page.getByRole('button', { name: /^Out of stock/ }).click();
    await expect.poll(() => new URL(page.url()).searchParams.get('stockStatus')).toBe('Out of Stock');
    await page.getByRole('button', { name: /^All/ }).first().click();
    await page.getByRole('searchbox').first().fill('UIA-TS-L');
    await expect.poll(() => visibleRowsText(page)).toContain('UI-AUDIT T-Shirt');
    await page.getByRole('button', { name: /Actions for UI-AUDIT T-Shirt/ }).first().click();
    await page.getByRole('menuitem', { name: 'Set stock' }).click();
    const sheet = page.getByRole('dialog');
    await sheet.getByLabel(/^Quantity/).fill('-1');
    await sheet.getByRole('button', { name: 'Save stock' }).click();
    await expect(sheet.getByText('Must be 0 or more.')).toBeVisible();
    await sheet.getByRole('button', { name: 'Cancel' }).click();
    await shot(page, `s4-${info.project.name}-stock`);
    expect(await seriousAxe(page, 'main')).toEqual([]);
  });

  expect(log.pageErrors).toEqual([]);
});

test('cashier: what a Biller can do on the pilot pages', async ({ page }, info) => {
  test.skip(!ROLES.cashier.email, 'set E2E_CASHIER_EMAIL and E2E_CASHIER_PASSWORD');
  test.setTimeout(120_000);
  await signIn(page, 'cashier');
  await go(page, '/customers');
  await settle(page);
  await expect(page.getByRole('button', { name: 'Add customer' })).toBeVisible(); // customers:create
  await page.getByRole('searchbox').first().fill('UI-AUDIT Customer');
  await expect.poll(() => visibleRowsText(page)).toContain('UI-AUDIT Customer');
  await expect(page.getByRole('button', { name: /Actions for UI-AUDIT Customer/ })).toHaveCount(0); // no edit/delete
  await go(page, '/categories');
  await expect(page.getByText("You don't have access to this page")).toBeVisible();
  await go(page, '/inventory');
  await settle(page);
  await expect(page.getByRole('button', { name: /^Actions for/ })).toHaveCount(0); // no inventory:edit
  await shot(page, `s4-cashier-${info.project.name}-stock`);
});
