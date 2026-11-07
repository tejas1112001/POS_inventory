// Browser tests against a running dev stack (see README.md).
import { defineConfig } from '@playwright/test';

const widths = [360, 768, 1280];

export default defineConfig({
  testDir: '.',
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 2,
  retries: 0,
  reporter: [['list'], ['json', { outputFile: '../test-results/e2e.json' }]],
  outputDir: '../test-results/artifacts',
  use: {
    baseURL: process.env.E2E_BASE_URL || 'https://demo-shop.localhost:8443',
    ignoreHTTPSErrors: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: widths.map((w) => ({
    name: `w${w}`,
    use: { viewport: { width: w, height: w === 360 ? 780 : w === 768 ? 1024 : 800 } },
  })),
});
