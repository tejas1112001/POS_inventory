// Route smoke test: every staff route at every width, for the owner and
// a Biller cashier. A route must render (no page error, no blank page,
// no 5xx, no sideways scrolling); routes a role may not open must say so.
import { test, expect } from '@playwright/test';
import { ROLES, go, settle, shot, signIn, watch } from './helpers';

const ROUTES = ['/dashboard', '/pos', '/sales', '/add-sale', '/sales-return', '/add-sales-return', '/invoices',
  '/products', '/add-product', '/inventory', '/stock-adjustments', '/purchases', '/add-purchase',
  '/categories', '/add-category', '/brands', '/add-brand', '/units', '/add-unit', '/suppliers', '/add-supplier',
  '/customers', '/add-customer', '/sales-report', '/purchase-report', '/customer-report', '/customer-due-report',
  '/showcase-settings', '/users', '/roles', '/add-role', '/billing', '/settings', '/no-such-page'];

// Routes the seeded Biller role may not open.
const CASHIER_DENIED = new Set(['/sales-return', '/add-sales-return', '/add-product', '/stock-adjustments', '/purchases',
  '/add-purchase', '/categories', '/add-category', '/brands', '/add-brand', '/units', '/add-unit', '/suppliers',
  '/add-supplier', '/users', '/roles', '/add-role', '/billing']);

for (const role of ['owner', 'cashier']) {
  test.describe(`${role} routes`, () => {
    test.skip(!ROLES[role].email, `set E2E_${role.toUpperCase()}_EMAIL and _PASSWORD`);

    test(`${role}: every route renders`, async ({ page }, info) => {
      test.setTimeout(240_000);
      const log = watch(page);
      await signIn(page, role);
      const problems = [];
      for (const route of ROUTES) {
        log.pageErrors.length = 0;
        log.serverErrors.length = 0;
        await go(page, route);
        await settle(page);
        const text = (await page.locator('#root').innerText()).trim();
        const sideways = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 2);
        await shot(page, `${role}-${info.project.name}${route.replace(/\//g, '_')}`);
        if (log.pageErrors.length) problems.push(`${route}: page error ${log.pageErrors[0]}`);
        if (log.serverErrors.length) problems.push(`${route}: ${log.serverErrors[0]}`);
        if (text.length < 20) problems.push(`${route}: blank page`);
        if (sideways) problems.push(`${route}: sideways scroll`);
        if (route === '/no-such-page' && !text.includes('Page not found')) problems.push(`${route}: no "Page not found"`);
        if (role === 'cashier' && CASHIER_DENIED.has(route) && !text.includes("You don't have access to this page")) {
          problems.push(`${route}: cashier not told about missing access`);
        }
      }
      expect(problems, problems.join('\n')).toEqual([]);
    });
  });
}
