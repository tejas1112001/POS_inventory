#!/bin/sh
# Runs the browser tests in the official Playwright image, attached to the
# dev stack's network (start it first: make up-web in backend-go).
#   E2E_OWNER_EMAIL=... E2E_OWNER_PASSWORD=... [E2E_CASHIER_EMAIL=... E2E_CASHIER_PASSWORD=...] \
#   [E2E_SHOTS=/repo/docs/ui-audit/slices/S0] frontend/e2e/run-in-docker.sh [playwright args]
set -eu
REPO=$(cd "$(dirname "$0")/../.." && pwd)
exec docker run --rm -i --network posd-dev_backend --ipc=host \
  --add-host admin.localhost:127.0.0.1 --add-host demo-shop.localhost:127.0.0.1 \
  -v "$REPO:/repo" -w /repo/frontend \
  -e E2E_OWNER_EMAIL -e E2E_OWNER_PASSWORD -e E2E_CASHIER_EMAIL -e E2E_CASHIER_PASSWORD -e E2E_SHOTS \
  -u "$(id -u):$(id -g)" -e HOME=/tmp \
  mcr.microsoft.com/playwright:v1.56.1-noble \
  sh -c 'node e2e/fwd.cjs & sleep 0.5; npx playwright test -c e2e/playwright.config.js "$@"' -- "$@"
