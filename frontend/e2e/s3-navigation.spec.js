// S3: grouped menu by role, phone tab bar, Reports tabs, shortcuts.
import { test, expect } from '@playwright/test';
import { ROLES, go, settle, shot, signIn } from './helpers';

const OWNER_GROUPS = ['Overview', 'Sell', 'Inventory', 'Contacts', 'Catalogue setup', 'Reports', 'Online shop', 'Team', 'Settings'];
const CASHIER_GROUPS = ['Overview', 'Sell', 'Inventory', 'Contacts', 'Reports', 'Settings'];

async function openMenu(page) {
  const width = page.viewportSize().width;
  if (width < 1024) await page.getByRole('button', { name: 'Open menu' }).click();
  return page.locator('aside nav');
}

for (const role of ['owner', 'cashier']) {
  test(`${role}: menu, tabs and reports`, async ({ page }, info) => {
    test.skip(!ROLES[role].email, `set E2E_${role.toUpperCase()}_EMAIL and _PASSWORD`);
    test.setTimeout(120_000);
    const width = page.viewportSize().width;
    await signIn(page, role);
    await go(page, '/customers');
    await settle(page);

    await test.step('menu groups match the role', async () => {
      const nav = await openMenu(page);
      const headings = (await nav.locator('h3').allTextContents()).map((h) => h.trim().toLowerCase());
      const want = (role === 'owner' ? OWNER_GROUPS : CASHIER_GROUPS).map((g) => g.toLowerCase());
      expect(headings).toEqual(want);
      await expect(nav.getByRole('link', { name: 'Customers' })).toHaveAttribute('aria-current', 'page');
      if (role === 'cashier') {
        await expect(nav.getByRole('link', { name: 'Storefront' })).toHaveCount(0);
        await expect(nav.getByRole('link', { name: 'Purchases' })).toHaveCount(0);
      }
      await shot(page, `s3-${role}-${info.project.name}-menu`);
      if (width < 1024) await page.getByRole('button', { name: 'Close sidebar' }).click();
    });

    await test.step('phone tab bar only under 768 px', async () => {
      const bar = page.getByRole('navigation', { name: 'Quick menu' });
      if (width < 768) {
        await expect(bar).toBeVisible();
        const tabs = (await bar.getByRole('link').allTextContents()).map((t) => t.trim());
        expect(tabs).toEqual(role === 'owner' ? ['Dashboard', 'Checkout', 'Orders'] : ['Checkout', 'Orders', 'Customers']);
        await bar.getByRole('button', { name: /More/ }).click();
        await expect(page.locator('aside nav')).toBeInViewport();
        await page.getByRole('button', { name: 'Close sidebar' }).click();
        // The page content is not hidden behind the bar.
        const pad = await page.locator('main').evaluate((m) => parseFloat(getComputedStyle(m).paddingBottom));
        expect(pad).toBeGreaterThanOrEqual(64);
      } else {
        await expect(bar).toBeHidden();
      }
    });

    await test.step('Reports: one entry, tabs by permission', async () => {
      await go(page, '/reports', '/sales-report');
      await settle(page);
      expect(new URL(page.url()).pathname).toBe('/sales-report');
      const tabs = (await page.getByRole('navigation', { name: 'Reports' }).getByRole('link').allTextContents()).map((t) => t.trim());
      expect(tabs).toEqual(role === 'owner' ? ['Sales', 'Purchases', 'Customers', 'Customer dues'] : ['Sales', 'Customers', 'Customer dues']);
      await page.getByRole('navigation', { name: 'Reports' }).getByRole('link', { name: 'Customer dues' }).click();
      await page.waitForURL('**/customer-due-report');
      await expect(page.locator('header h1')).toHaveText('Reports');
      await shot(page, `s3-${role}-${info.project.name}-reports`);
    });

    await test.step('keyboard: g then o opens Orders', async () => {
      await page.evaluate(() => document.activeElement?.blur());
      await page.keyboard.press('g');
      await page.keyboard.press('o');
      await page.waitForURL('**/sales');
      await expect(page.locator('header h1')).toHaveText('Orders');
    });
  });
}
