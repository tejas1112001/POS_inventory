// Writes a ready-made HTML file for every public page after `vite build`:
//   dist/index.html, dist/features/index.html, dist/pricing/index.html, ...
// Each file has the page's own title, description, canonical, social tags,
// structured data and the rendered page body, so crawlers and link previews
// do not need JavaScript. The server (backend/internal/httpx/static.go)
// serves dist/<page>/index.html for /<page>.
//
// The build stops with an error if a page breaks a check below.

import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const dist = path.join(root, 'dist');
const ssrDir = path.join(root, 'dist-ssr');

const { render, headTags, routes } = await import(pathToFileURL(path.join(ssrDir, 'entry-server.js')).href);
const template = await fs.readFile(path.join(dist, 'index.html'), 'utf8');

const SEO_BLOCK = /<!--seo:start[\s\S]*?<!--seo:end-->/;
if (!SEO_BLOCK.test(template)) throw new Error('prerender: no <!--seo:start--> block in dist/index.html');
if (!template.includes('<div id="root"></div>')) throw new Error('prerender: no empty #root in dist/index.html');

const problems = [];
const titles = new Map();
const descriptions = new Map();

function check(url, html) {
  const fail = (msg) => problems.push(`${url}: ${msg}`);
  const one = (re, what) => {
    const n = (html.match(re) || []).length;
    if (n !== 1) fail(`${n} ${what} (want 1)`);
  };
  one(/<h1[\s>]/g, '<h1>');
  one(/<title>/g, '<title>');
  one(/<meta name="description"/g, 'meta description');
  one(/<link rel="canonical"/g, 'canonical link');
  one(/<script type="application\/ld\+json">/g, 'JSON-LD block');

  const title = html.match(/<title>([^<]*)<\/title>/)?.[1] ?? '';
  const desc = html.match(/<meta name="description" content="([^"]*)"/)?.[1] ?? '';
  if (title.length > 65) fail(`title is ${title.length} characters (keep it under 65)`);
  if (desc.length < 70 || desc.length > 160) fail(`description is ${desc.length} characters (want 70 to 160)`);
  if (titles.has(title)) fail(`same title as ${titles.get(title)}`);
  if (descriptions.has(desc)) fail(`same description as ${descriptions.get(desc)}`);
  titles.set(title, url);
  descriptions.set(desc, url);

  // The strict CSP (style-src 'self') would block style="" attributes.
  if (/<[^>]+\sstyle="/.test(html)) fail('has a style="" attribute (blocked by the CSP); see src/lib/useHydrated.js');
  if (/\[(PHONE NUMBER|EMAIL|PRICE|N|X)\]/.test(title + desc)) fail('placeholder in title or description');
  if (/—/.test(html.replace(/<script[\s\S]*?<\/script>/g, ''))) fail('em dash in the page text');

  // Inline scripts are blocked by the CSP; only JSON-LD data may be inline.
  for (const [tag] of html.matchAll(/<script\b[^>]*>/g)) {
    if (!/type="application\/ld\+json"/.test(tag) && !/\ssrc="/.test(tag)) fail(`inline script ${tag}`);
  }
  if (/aria-busy="true"|<template id="B:/.test(html)) fail('a loading placeholder was rendered instead of the page');

  // The first heading is the h1, and headings do not skip a level (h2 then h4).
  if (!/^<h1[\s>]/.test(html.match(/<h[1-6][\s>]/)?.[0] ?? '')) fail('the first heading is not the h1');
  let prev = 1;
  for (const [, lvl] of html.matchAll(/<h([1-6])[\s>]/g)) {
    const n = Number(lvl);
    if (n > prev + 1) fail(`heading jumps from h${prev} to h${n}`);
    prev = n;
  }

  const ld = html.match(/<script type="application\/ld\+json">([\s\S]*?)<\/script>/)?.[1];
  try {
    const data = JSON.parse(ld);
    if (data['@context'] !== 'https://schema.org' || !Array.isArray(data['@graph'])) fail('JSON-LD has no schema.org @graph');
  } catch (e) {
    fail(`JSON-LD does not parse: ${e.message}`);
  }
}

for (const url of routes) {
  const body = await render(url);
  const html = template.replace(SEO_BLOCK, headTags(url)).replace('<div id="root"></div>', `<div id="root">${body}</div>`);
  check(url, html);
  const out = url === '/' ? path.join(dist, 'index.html') : path.join(dist, url.slice(1), 'index.html');
  await fs.mkdir(path.dirname(out), { recursive: true });
  await fs.writeFile(out, html);
  console.log(`prerendered ${url.padEnd(12)} → ${path.relative(root, out)} (${(html.length / 1024).toFixed(1)} kB)`);
}

await fs.rm(ssrDir, { recursive: true, force: true });

if (problems.length) {
  console.error(`\nprerender: ${problems.length} SEO check(s) failed:\n  ${problems.join('\n  ')}`);
  process.exit(1);
}
console.log(`prerender: ${routes.length} pages, all SEO checks passed`);
