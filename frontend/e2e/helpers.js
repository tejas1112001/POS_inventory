// Shared helpers for the browser tests.
import fs from 'node:fs';
import path from 'node:path';

export const ROLES = {
  owner: { email: process.env.E2E_OWNER_EMAIL, password: process.env.E2E_OWNER_PASSWORD },
  cashier: { email: process.env.E2E_CASHIER_EMAIL, password: process.env.E2E_CASHIER_PASSWORD },
};

// watch records page errors (uncaught exceptions) and 5xx answers.
export function watch(page) {
  const log = { pageErrors: [], serverErrors: [], consoleErrors: [] };
  page.on('pageerror', (e) => log.pageErrors.push(String(e.message || e)));
  page.on('console', (m) => m.type() === 'error' && log.consoleErrors.push(m.text()));
  page.on('response', (r) => r.status() >= 500 && log.serverErrors.push(`${r.status()} ${r.url()}`));
  return log;
}

export async function signIn(page, role) {
  const { email, password } = ROLES[role];
  await page.goto('/login');
  await page.fill('#email', email);
  await page.fill('#password', password);
  await page.click('button[type=submit]');
  // The suite signs in more often than people do: when the server's login
  // limit answers "Too many attempts", wait the minute out and try again.
  const limited = page.getByText(/Too many attempts/);
  const outcome = await Promise.race([
    page.waitForURL((u) => !u.pathname.startsWith('/login'), { timeout: 20_000 }).then(() => 'in'),
    limited.waitFor({ timeout: 20_000 }).then(() => 'limited'),
  ]);
  if (outcome === 'limited') {
    await page.waitForTimeout(61_000);
    await page.click('button[type=submit]');
    await page.waitForURL((u) => !u.pathname.startsWith('/login'), { timeout: 20_000 });
  }
  // The app may still redirect once (to the page asked for, or the
  // dashboard): wait until the layout is up and the page is quiet.
  await page.locator('main').first().waitFor({ timeout: 20_000 });
  await settle(page);
}

// settle waits for the page to finish loading its data.
export async function settle(page) {
  try {
    await page.waitForLoadState('networkidle', { timeout: 3_000 });
  } catch {
    /* some pages keep a request open */
  }
  await page.waitForTimeout(500);
}

// shot saves a screenshot under E2E_SHOTS (if set) for the slice review.
export async function shot(page, name) {
  const dir = process.env.E2E_SHOTS;
  if (!dir) return;
  fs.mkdirSync(dir, { recursive: true });
  await page.screenshot({ path: path.join(dir, `${name}.jpg`), type: 'jpeg', quality: 55 });
}

// go moves to a route inside the app without reloading the page, as a
// click on a link would. A full reload restores the session from the
// refresh cookie each time, and the server rate-limits that per address.
// landing: where the route is expected to end up (a redirect), if not itself.
export async function go(page, route, landing = route) {
  for (let attempt = 0; attempt < 3; attempt += 1) {
    await page.evaluate((r) => {
      window.history.pushState({}, '', r);
      window.dispatchEvent(new PopStateEvent('popstate'));
    }, route);
    await page.waitForTimeout(300);
    if (new URL(page.url()).pathname === landing) return;
  }
  throw new Error(`could not open ${route} (expected ${landing}, now at ${new URL(page.url()).pathname})`);
}
