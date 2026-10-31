# UI build log (step 2)

One entry per slice: what changed, how it was tested, the result. Screenshots of every page at 360, 768 and 1280 px for the owner and the UI-AUDIT Cashier are in `slices/<slice>/`. Tests run with `frontend/e2e/run-in-docker.sh` against the dev stack.

## S0 – Safety net and CI

**Changed**
- Add Product no longer crashes (missing `usePermission` import; `isAdmin` replaced by the `products:create` permission).
- `ErrorBoundary` around the whole app (`Root.jsx`) and around the page area (`AppLayout`, reset on navigation). A crash now shows "Something went wrong on this page" with Try again / Go to Dashboard / Copy details; a stale code chunk after a deploy shows "A new version is available". Same safety net in the Super Admin console (CSS classes only).
- Forbidden pages show "You don't have access to this page" inside the app instead of silently redirecting; unknown addresses show "Page not found" (signed out: sign-in first).
- Lint: 37 problems → 0 errors (18 warnings remain: hook dependency and hot-reload hints). Build output and Node files are no longer linted as browser code.
- New CI workflow `.github/workflows/frontend.yml`: install, lint (errors fail), build the shop app and the console.
- New browser test suite `frontend/e2e/` (Playwright 1.56.1, axe for later slices), run in the official Playwright container.
- `chart.js` declared; `react-hook-form`, `@playwright/test`, `@axe-core/playwright` added (dev tools only for the last two).

**Tested**: `e2e/smoke.spec.js`, owner and cashier × 360/768/1280 (6 runs, 34 routes each): no page errors, no blank pages, no 5xx, no sideways scrolling, "Page not found" on unknown routes, "no access" on every route the Biller may not open. **6 passed.**

Note: the test navigates inside the app (like clicking links). Full page reloads restore the session each time and hit the server's per-address rate limit on session refresh.
