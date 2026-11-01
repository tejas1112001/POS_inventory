// S2: one message system (toasts, confirm dialogs), no browser pop-ups,
// and the Super Admin console still clean under its strict CSP.
import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { ROLES, go, settle, shot, signIn, watch } from './helpers';

test.describe('shop app messages', () => {
  test.skip(!ROLES.owner.email, 'set E2E_OWNER_EMAIL and E2E_OWNER_PASSWORD');

  test('toasts and confirm dialogs', async ({ page }, info) => {
    test.setTimeout(180_000);
    const log = watch(page);
    const popups = [];
    page.on('dialog', async (d) => {
      popups.push(`${d.type()}: ${d.message()}`);
      await d.dismiss();
    });
    const width = page.viewportSize().width;
    const name = `E2E-S2 Category ${info.project.name} ${Date.now().toString(36)}`;
    await signIn(page, 'owner');

    await test.step('success toast after saving, inside the screen', async () => {
      await go(page, '/add-category');
      await settle(page);
      await page.fill('input[name=name]', name);
      await page.getByRole('button', { name: 'Add Category' }).click();
      await page.waitForURL('**/categories');
      const toast = page.locator('.toast--success', { hasText: 'Category added.' });
      await expect(toast).toBeVisible();
      await expect(page.getByRole('status', { name: 'Notifications' })).toContainText('Category added.');
      const box = await toast.boundingBox();
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(width);
      if (width < 640) expect(box.y).toBeLessThan(140); // top on phones, clear of the Save buttons
      await page.waitForTimeout(400); // let the entry animation finish
      await shot(page, `s2-${info.project.name}-toast-success`);
      const axe = await new AxeBuilder({ page }).include('.toast-viewport').analyze();
      expect(axe.violations.filter((v) => ['serious', 'critical'].includes(v.impact)).map((v) => v.id)).toEqual([]);
    });

    await test.step('error toast for a duplicate, closed with the × button', async () => {
      await go(page, '/add-category');
      await settle(page);
      await page.fill('input[name=name]', name);
      await page.getByRole('button', { name: 'Add Category' }).click();
      const toast = page.locator('.toast--error', { hasText: 'already exists' });
      await expect(toast).toBeVisible();
      await expect(page.getByRole('alert', { name: 'Errors' })).toContainText('already exists');
      const box = await toast.boundingBox();
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(width);
      await page.waitForTimeout(400);
      await shot(page, `s2-${info.project.name}-toast-error`);
      await toast.getByRole('button', { name: 'Close message' }).click();
      await expect(toast).toBeHidden();
    });

    await test.step('delete asks in an app dialog (cancelled: nothing deleted)', async () => {
      await go(page, '/roles');
      await settle(page);
      await page.fill('input[placeholder="Search roles..."]', 'UI-AUDIT Role');
      await page.getByRole('button', { name: 'Delete', exact: true }).first().click();
      const dialog = page.getByRole('dialog');
      await expect(dialog).toContainText('Delete the role "UI-AUDIT Role"?');
      await page.waitForTimeout(400);
      await shot(page, `s2-${info.project.name}-confirm`);
      const axe = await new AxeBuilder({ page }).include('[role=dialog]').analyze();
      expect(axe.violations.filter((v) => ['serious', 'critical'].includes(v.impact)).map((v) => v.id)).toEqual([]);
      await dialog.getByRole('button', { name: 'Cancel' }).click();
      await expect(dialog).toBeHidden();
      await expect(page.getByText('UI-AUDIT Role').first()).toBeVisible();
    });

    await test.step('web-address change asks in an app dialog', async () => {
      await go(page, '/showcase-settings');
      await settle(page);
      const slug = page.locator('input[placeholder="e.g. abc-fashion"]');
      const before = await slug.inputValue();
      await slug.fill(`${before}-e2e`);
      await page.getByRole('button', { name: 'Save Profile Changes' }).click();
      const dialog = page.getByRole('dialog');
      await expect(dialog).toContainText('Change the web address?');
      await dialog.getByRole('button', { name: 'Cancel' }).click();
      await slug.fill(before);
    });

    expect(popups, 'browser pop-ups').toEqual([]);
    expect(log.pageErrors).toEqual([]);
  });
});

test.describe('Super Admin console under its CSP', () => {
  test('no CSP violations, no run-time <style>, shared styles and font load', async ({ page }) => {
    const violations = [];
    page.on('console', (m) => /Content Security Policy|Refused to/.test(m.text()) && violations.push(m.text()));
    await page.goto('https://admin.localhost:8443/');
    await page.waitForLoadState('networkidle', { timeout: 5_000 }).catch(() => {});
    const report = await page.evaluate(() => ({
      styleTags: document.querySelectorAll('style').length,
      inlineStyled: document.querySelectorAll('[style]').length,
      tokens: getComputedStyle(document.documentElement).getPropertyValue('--ds-danger').trim(),
      font: getComputedStyle(document.body).fontFamily,
    }));
    expect(violations).toEqual([]);
    expect(report.styleTags).toBe(0);
    expect(report.inlineStyled).toBe(0);
    expect(report.tokens).not.toBe('');
    expect(report.font).toContain('Inter');
  });
});
