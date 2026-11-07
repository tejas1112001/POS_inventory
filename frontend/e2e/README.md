# Browser tests

Playwright tests that drive the real app at 360, 768 and 1280 px.

1. Start the dev stack: `cd backend-go && make up-web` (after `npm run build` and `npm run build:admin` in `frontend`).
2. Run, with the demo-shop owner (and optionally a Biller cashier):

   ```sh
   E2E_OWNER_EMAIL=owner@demo.test E2E_OWNER_PASSWORD=... \
   E2E_CASHIER_EMAIL=... E2E_CASHIER_PASSWORD=... \
   frontend/e2e/run-in-docker.sh
   ```

   `E2E_SHOTS=/repo/docs/ui-audit/slices/<slice>` also saves a screenshot of every page.

The tests run in the official Playwright image so no browser has to be installed on the host. Results go to `frontend/test-results/` (not committed).
