// Build-time renderer (scripts/prerender.mjs): turns each public page into
// static HTML so search engines and link previews see the full page
// without running JavaScript. The browser then hydrates it (main.jsx).

import { StrictMode } from 'react';
import { prerenderToNodeStream } from 'react-dom/static';
import { StaticRouter } from 'react-router-dom';
import App from './App';

export { headTags, routes } from '@/content/seo';

/** The HTML of the page at `url`, with every lazy page loaded. */
export async function render(url) {
  const { prelude } = await prerenderToNodeStream(
    <StrictMode>
      <StaticRouter location={url}>
        <App />
      </StaticRouter>
    </StrictMode>,
    // One piece of HTML in page order: no fallback first with the content
    // swapped in later by an inline script (the CSP blocks those).
    { progressiveChunkSize: Number.POSITIVE_INFINITY },
  );
  let html = '';
  for await (const chunk of prelude) html += chunk;
  return html;
}
