// Draws the site's raster brand images into public/assets with the
// Playwright Chromium the e2e tests already use. Run it by hand after a
// brand change, then bump the -vN in the file names (and in
// src/content/seo.js and index.html), because /assets/* is cached for a year:
//
//   node landing/scripts/brand-images.mjs
//
// og-mintobill-v1.png       1200×630  social preview (WhatsApp, LinkedIn, X)
// mintobill-logo-512.png    512×512   logo for search engines (JSON-LD)
// apple-touch-icon-v1.png   180×180   phone home-screen icon

import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const require = createRequire(import.meta.url);
const { chromium } = require('playwright-core'); // the version whose browser is installed

const here = path.dirname(fileURLToPath(import.meta.url));
const out = path.resolve(here, '../public/assets');
const font = fs.readFileSync(require.resolve('@fontsource-variable/manrope/files/manrope-latin-wght-normal.woff2')).toString('base64');

// The MintoBill "M" (as in public/favicon.svg and src/components/ui/Logo.jsx).
const mark = (size) => `
<svg width="${size}" height="${size}" viewBox="0 0 40 40" fill="none" xmlns="http://www.w3.org/2000/svg">
  <defs>
    <linearGradient id="a" x1="4" y1="34" x2="22" y2="6" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#123b45"/><stop offset="1" stop-color="#17807a"/></linearGradient>
    <linearGradient id="b" x1="18" y1="6" x2="36" y2="34" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#17807a"/><stop offset="1" stop-color="#20b6a4"/></linearGradient>
  </defs>
  <rect width="40" height="40" rx="11" fill="#f4f2e9"/>
  <g transform="translate(5.5 5.2) scale(0.72)">
    <path d="M8 33V15.5a6.5 6.5 0 0 1 13 0V27" stroke="url(#a)" stroke-width="6.5" stroke-linecap="round" stroke-linejoin="round"/>
    <path d="M21 15.5a6.5 6.5 0 0 1 13 0V33" stroke="url(#b)" stroke-width="6.5" stroke-linecap="round" stroke-linejoin="round"/>
  </g>
</svg>`;

const base = `
<style>
  @font-face { font-family: Manrope; src: url(data:font/woff2;base64,${font}) format('woff2'); font-weight: 200 800; }
  * { margin: 0; box-sizing: border-box; }
  body { font-family: Manrope, sans-serif; }
</style>`;

const og = `${base}
<style>
  .card { width: 1200px; height: 630px; padding: 76px 84px; color: #dbe8e6; position: relative; overflow: hidden;
    background: radial-gradient(900px 520px at 88% 8%, rgba(32,182,164,.28), transparent 60%), #0b2a32; display: flex; flex-direction: column; }
  .brand { display: flex; align-items: center; gap: 22px; }
  .word { font-size: 54px; font-weight: 800; letter-spacing: -0.02em; color: #fff; }
  .word span { color: #3fd0bd; }
  h1 { margin-top: 64px; font-size: 76px; line-height: 1.04; font-weight: 800; letter-spacing: -0.035em; color: #fff; max-width: 900px; }
  h1 span { color: #3fd0bd; }
  .chips { margin-top: auto; display: flex; gap: 14px; }
  .chip { border: 2px solid rgba(140,165,165,.35); border-radius: 999px; padding: 12px 24px; font-size: 26px; font-weight: 600; }
  .url { position: absolute; right: 84px; top: 92px; font-size: 26px; font-weight: 700; color: #7fd6c8; }
</style>
<div class="card">
  <div class="brand">${mark(84)}<div class="word">Minto<span>Bill</span></div></div>
  <p class="url">mintobill.com</p>
  <h1>GST billing and stock<br><span>for Indian shops</span></h1>
  <div class="chips"><p class="chip">GST bills</p><p class="chip">Stock and purchases</p><p class="chip">Reports</p><p class="chip">Runs in the browser</p></div>
</div>`;

const icon = (size, pad) => `${base}
<style> body { width: ${size}px; height: ${size}px; background: #0b2a32; display: flex; align-items: center; justify-content: center; } </style>
${mark(size - pad * 2)}`;

// Playwright's Chromium first. On WSL without its system libraries, fall
// back to the Windows Edge in headless mode (set EDGE to another browser).
const EDGE = process.env.EDGE || '/mnt/c/Program Files (x86)/Microsoft/Edge/Application/msedge.exe';
const winPath = (p) => execFileSync('wslpath', ['-w', p]).toString().trim();

let browser = null;
try {
  browser = await chromium.launch();
} catch (e) {
  if (!fs.existsSync(EDGE)) throw e;
  console.log('Playwright Chromium did not start; using', EDGE);
}

const shoot = async (html, w, h, file) => {
  const dest = path.join(out, file);
  if (browser) {
    const page = await browser.newPage({ viewport: { width: w, height: h } });
    await page.setContent(html);
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot({ path: dest, type: 'png' });
    await page.close();
  } else {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'brand-'));
    const src = path.join(tmp, 'page.html');
    fs.writeFileSync(src, `<!doctype html><meta charset="utf-8">${html}`);
    execFileSync(EDGE, ['--headless=new', '--disable-gpu', '--hide-scrollbars', '--force-device-scale-factor=1',
      `--window-size=${w},${h}`, `--screenshot=${winPath(dest)}`, 'file:' + winPath(src).replace(/\\/g, '/')], { stdio: 'ignore' });
    fs.rmSync(tmp, { recursive: true, force: true });
  }
  console.log('wrote', path.join('public/assets', file));
};
fs.mkdirSync(out, { recursive: true });
await shoot(og, 1200, 630, 'og-mintobill-v1.png');
await shoot(icon(512, 56), 512, 512, 'mintobill-logo-512.png');
await shoot(icon(180, 18), 180, 180, 'apple-touch-icon-v1.png');
await browser?.close();
