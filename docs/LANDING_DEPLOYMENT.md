# Landing Site Deployment

Status as of 2026-11-19: **the React landing site (`landing/`) is not deployed yet.** Nothing in the repo builds or ships it. This note records what is live today, how the site must be served, and what is left before it can replace the current pages.

## 1. What is live today

The main address (`<BASE_DOMAIN>`, host kind `HostApex`) is served by the Go app's own server-rendered pages in `backend-go/internal/landing`: Go templates plus an embedded `static/site.css`. The React BillPiko marketing site in `landing/` is separate and not wired into the app, Caddy or CI.

CI today: `.github/workflows/frontend.yml` (shop app and Super Admin console) and `go-backend.yml`. Neither touches `landing/`.

## 2. Dev server vs production build

| | Dev server | Production build |
|---|---|---|
| Command | `npm run dev:landing` (or `npm run dev` in `landing/`) | `npm run build:landing` → `landing/dist/` |
| Local URL | `http://localhost:5174` | `npx vite preview --port 4174` in `landing/` → `http://localhost:4174` |
| Use | Working on the site (instant reload) | **The only thing that is deployed** |

The dev server must never be what visitors get. It serves unbundled modules, React's development build and the hot-reload client.

**Measuring performance:** run Lighthouse against the production build (`localhost:4174`), in an Incognito window so browser extensions do not add their own scripts to the trace. Results on 2026-11-19, desktop preset: production build 100; dev server 99–100 in clean headless Chrome. Your own browser can show lower numbers on the dev server.

## 3. Serving `landing/dist/` on the main address

Two options:

- **(a) The Go app serves it (recommended).** This is the same pattern as the shop app and console: `STATIC_DIR` with `tenant/` and `admin/` mounts in `backend-go/deploy/docker-compose.web.yml`. Add a `landing/` mount and serve it on `HostApex`. The site then shares one origin with the Go form handlers and gets the same security headers from one place.
- **(b) Caddy `file_server`** for the apex host, with the Go-owned paths (section 4) reverse-proxied to the app.

Whichever option is chosen:

1. **SPA fallback.** Any path that is not a file and not a Go-owned path must return `landing/dist/index.html`. Otherwise `/pricing`, `/features` and others return 404 on refresh or direct visit.
2. **Real 404s.** With a fallback, unknown paths render the React "page not found" screen but return HTTP 200 (a soft 404 for search engines). Either return 404 for paths outside the known route list (`/`, `/features`, `/industries`, `/pricing`, `/contact`), or accept the soft 404.
3. **Caching.** Everything under `/assets/` has a content hash in its name, so serve it with `Cache-Control: public, max-age=31536000, immutable`. Serve `index.html` with `no-cache` so a deploy is picked up at once.
4. **Build-time settings.** `VITE_BASE_DOMAIN` and `VITE_PLATFORM_NAME` (`landing/src/lib/config.js`) are baked in when the site is built. Set them in the build environment, not at run time.

## 4. Paths the Go app keeps on the main address

The React site links to these and expects the Go app to keep serving them (`backend-go/internal/landing/landing.go`):

- `/terms`, `/privacy` (footer links)
- `/login`, `/login/go` (staff sign-in redirect to the shop's address)
- `/find-my-shop`, `POST /find-my-shop`, `/find-my-shop/sent` (linked from the Contact page)
- `POST /contact`, `/contact/sent`
- `/robots.txt`, `/sitemap.xml`, `/favicon.svg`

**Conflict: `GET /contact`.** Both the Go app (template contact page) and the React site (`/contact` route with the demo form) use it. Decide which one owns it before go-live. If React owns it, the Go handler keeps only `POST /contact`.

`/sitemap.xml` must list the React routes (`/features`, `/industries`, `/pricing`, `/contact`) once they are live.

## 5. Open work before go-live

- [ ] **CI workflow for `landing/`**: lint and build on push and PR, like `frontend.yml`. The root `package.json` already has the `landing` workspace and `build:landing`.
- [ ] **Serve the build** as in section 3 (mount, SPA fallback, caching).
- [ ] **Wire the demo form.** `landing/src/sections/contact/DemoForm.jsx` only shows a success message; the submit has a `TODO` and sends nothing. It needs to post to the Go `POST /contact` handler, which expects the landing form token. That means a JSON variant of the endpoint, or a way for the SPA to fetch a token.
- [ ] **Check the CSP.** The main address sends a strict policy (`docs/OPERATIONS.md` §7): `script-src 'self'; style-src 'self'`, no inline script or style. The Vite build emits external JS and CSS files, and the site's runtime styles are set through the DOM (React `style` props, `element.style`), which this policy allows. Load the deployed build with the real headers and confirm the browser console shows no CSP violations before switching over.
- [ ] **Resolve the `GET /contact` conflict** (section 4) and update `/sitemap.xml`.
- [ ] **Retire the Go template home page** (`s.home` on `/`) once the React site serves `/`.
