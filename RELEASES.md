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

| Tag | Date | Commit | Server id | Status |
|---|---|---|---|---|
| `v1.2.1` | 2026-11-24 | `ad12ab6` | `20261124112651-ad12ab6` | **Live** |
| `v1.2.0` | 2026-11-24 | `2c74dc8` | `20261124100645-2c74dc8` | Replaced (rollback target) |
| `v1.1.0` | 2026-11-24 | `28cb2c1` | `20261124032824-28cb2c1` | Replaced |
| `v1.0.0` | 2026-11-22 | `08024c4` | `20261122-08024c4-landing` | Replaced |

## v1.2.1 (live)

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
