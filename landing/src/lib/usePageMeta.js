import { useEffect } from 'react';
import { OG_IMAGE, ORIGIN, canonical, pages } from '@/content/seo';

const NOT_FOUND = { title: 'Page not found | MintoBill' };

// Find or create a <meta>/<link> tag in <head> and set one attribute.
function setTag(selector, create, attr, value) {
  let el = document.head.querySelector(selector);
  if (!el) {
    el = document.createElement(create.tag);
    Object.entries(create.attrs).forEach(([k, v]) => el.setAttribute(k, v));
    document.head.appendChild(el);
  }
  el.setAttribute(attr, value);
}

/**
 * Keeps the title, description, canonical and social tags in step with the
 * page after client-side navigation. The first load already has them: the
 * build writes them into every page (src/content/seo.js). `path` is a key of
 * `pages`; anything else is treated as the 404 page.
 */
export function usePageMeta(path) {
  useEffect(() => {
    const p = pages[path];
    if (!p) {
      document.title = NOT_FOUND.title;
      return;
    }
    const url = canonical(path);
    document.title = p.title;
    const meta = (key, name, value) => setTag(`meta[${key}="${name}"]`, { tag: 'meta', attrs: { [key]: name } }, 'content', value);
    meta('name', 'description', p.description);
    meta('property', 'og:title', p.title);
    meta('property', 'og:description', p.description);
    meta('property', 'og:url', url);
    meta('property', 'og:image', ORIGIN + OG_IMAGE.path);
    meta('name', 'twitter:title', p.title);
    meta('name', 'twitter:description', p.description);
    setTag('link[rel="canonical"]', { tag: 'link', attrs: { rel: 'canonical' } }, 'href', url);
  }, [path]);
}
