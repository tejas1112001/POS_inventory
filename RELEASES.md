# Releases

Production releases of MintoBill (mintobill.com), numbered with Semantic
Versioning (https://semver.org): `vMAJOR.MINOR.PATCH`. MAJOR for changes that
break existing shops, data or integrations; MINOR for new features; PATCH for
fixes and small layout changes. Each release has a Git tag on GitHub and a
folder on the VPS under `/var/www/bilmitra/releases/<server id>`
(the server still uses the BilMitra folder and service names until
`migrate-to-mintobill.sh` is run; see docs/DEPLOY_VPS.md §8).

Rolling back means pointing `/var/www/bilmitra/current` at an older server
folder and restarting `bilmitra` (the VPS keeps the five newest folders).

## How to make a release (every time)

1. **Pick the version** with Semantic Versioning, counting from the live
   release:
   - PATCH (`v1.2.1` → `v1.2.2`): fixes, copy and layout changes, nothing new
     for the user to learn.
   - MINOR (`v1.2.1` → `v1.3.0`): new features or screens; existing shops,
     data and integrations keep working. Additive database migrations are
     fine.
   - MAJOR (`v1.x` → `v2.0.0`): a change that breaks existing shops, data,
     the API or integrations, or needs manual work from shop owners.
   - Never reuse or move a version number. Tags are `v` + the number
     (`v1.3.0`), on the merged commit on `main`.
2. **Build and deploy** that commit (docs/DEPLOY_VPS.md §3). Build with the
   version in the binary (`buildinfo.Version`) so the server log names it.
3. **Write the notes here first**: a row in the table below (tag, date,
   commit, server folder, status) and a `## vX.Y.Z` section with what
   changed and whether there are database migrations. Mark the previous
   release Replaced and the new one **Live** once `/readyz` says ready.
4. **Publish it on GitHub as a Release, not only a tag**: an annotated tag
   plus a GitHub Release with the same title and this section as its notes,
   marked *Latest*:

   ```sh
   git tag -a vX.Y.Z <commit> -m "vX.Y.Z: <one-line summary>"
   git push origin vX.Y.Z
   gh release create vX.Y.Z --verify-tag --title "vX.Y.Z" --notes-file <notes.md> --latest
   ```

   (Or on GitHub: Releases → Draft a new release → pick the tag → paste the
   notes.) Attaching the release tarball is optional.
5. Commit `RELEASES.md` and update `status.md`.

## History

| Tag | Date | Commit | Server id | Status |
|---|---|---|---|---|
| `v1.3.0` | 2026-11-26 | `ea77600` | `20261126093818-ea77600` | **Live** |
| `v1.2.1` | 2026-11-24 | `ad12ab6` | `20261124112651-ad12ab6` | Replaced (rollback target) |
| `v1.2.0` | 2026-11-24 | `2c74dc8` | `20261124100645-2c74dc8` | Replaced |
| `v1.1.0` | 2026-11-24 | `28cb2c1` | `20261124032824-28cb2c1` | Replaced |
| `v1.0.0` | 2026-11-22 | `08024c4` | `20261122-08024c4-landing` | Replaced |

## v1.3.0 (live)

UI audit fixes, SEO landing pages and a checkout fix. No database changes.
Built off the server (Go in Docker) and installed with `install-release.sh`
(the same steps as `deploy.sh`).

- Reports: one filter line on every tab (search, Add filter, one date range,
  Excel / PDF / Print); the Sales tab scrolls and keeps the table usable; its
  payment-method box folds and follows the tab's dates and store; Customers
  and Customer dues no longer have overlapping filters; GST returns and
  Expiry use the app's dropdowns.
- App: low-stock banner only on Dashboard and Stock; sentence-case labels and
  statuses; grouped numbers on Stock; Invoices on the Add-filter toolbar;
  square checkboxes; clear disabled buttons; Settings no longer repeats
  Stores and Plan & billing; dashboard greeting, average bill from net
  sales, even card rows; checkout teal add buttons, no Clear on an empty
  cart, SKU shown for products with the same name; stronger dark-mode
  borders and banner.
- Landing site: SEO-ready pages built at build time (the old server-rendered
  home page is removed), rewritten copy, real contact details.
- Checkout: the quote is read after it is set.

## v1.2.1

Landing site layout fixes. No database changes.

- Content column up to 1344 px (container 1440 px) instead of 1200 px, so
  laptops and wide screens no longer show large empty sides.
- Sticky header; the phone menu opens over the page with a dimmed backdrop
  instead of pushing the page down, and locks page scroll while open.

## v1.2.0

Teal & Cream redesign and console-only business type. No database changes.

- Shop app and platform console on the Teal & Cream theme (Manrope, new logo),
  light and dark mode.
- Uniform page layout and spacing; filter toolbar on every list page; date
  range picker for every date filter; reusable tab bar; tabs, panels and
  tables laid out for phones; dialogs no longer hide fields.
- Business type is set only from the platform console (new **Change business
  type** action, audited); shops see it read-only and still choose modules.
- Landing site on the Teal & Cream theme and responsive on phones, tablets
  and laptops.
- Design references in `frontend/ui_images/`.

## v1.1.0

Pharmacy strict MRP and the MintoBill rename. Tenant migration 00046.

- Pharmacy: MRP printed per pack; selling above MRP is blocked (PR #29).
- BilMitra renamed to MintoBill across the app, landing site and docs
  (domain mintobill.com).
- Decimal quantities on the remaining screens.
- VPS deploy files (PR #28).

## v1.0.0

First production release: the landing release (server folder
`20261122-08024c4-landing`) at the merge of PR #27 (seed tool) and the
dependency updates (#21, #22, #25).
