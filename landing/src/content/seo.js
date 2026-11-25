// Search and social data for every public page: title, description,
// canonical address, preview image and structured data (JSON-LD).
// The build (scripts/prerender.mjs) writes these into each page's HTML;
// usePageMeta keeps the tags in step when visitors move between pages.

import { PLATFORM_NAME, BASE_DOMAIN } from '@/lib/config';
import { contact, faqs } from '@/content/site';

export const ORIGIN = `https://${BASE_DOMAIN}`;

// Files in public/assets. Change the file name when the image changes,
// because /assets/* is cached for a year.
export const OG_IMAGE = { path: '/assets/og-mintobill-v1.png', width: 1200, height: 630, alt: 'MintoBill: GST billing and stock for Indian shops' };
export const LOGO = '/assets/mintobill-logo-512.png';

export const pages = {
  '/': {
    title: 'MintoBill: GST Billing and POS Software for Indian Shops',
    description:
      'Make GST bills, keep stock up to date and track sales from any browser. MintoBill is point of sale software for Indian shops, with a 14-day free trial.',
  },
  '/features': {
    title: 'Features: GST Billing, Stock and Reports | MintoBill',
    description:
      'Fast checkout, CGST, SGST and IGST worked out on every bill, stock history, customer credit, Excel reports and an online catalogue for your shop.',
    crumb: 'Features',
  },
  '/industries': {
    title: 'POS for Clothing, Pharmacy, Grocery and More | MintoBill',
    description:
      'Billing and stock software for clothing, footwear, pharmacy, grocery, electronics and other retail shops. See which MintoBill tools suit your shop.',
    crumb: 'Industries',
  },
  '/pricing': {
    title: 'Pricing and Plans | MintoBill',
    description:
      'MintoBill plans for small and growing shops. Every plan includes GST bills, stock, reports and a 14-day free trial.',
    crumb: 'Pricing',
  },
  '/contact': {
    title: 'Contact Us and Book a Free Demo | MintoBill',
    description:
      'Book a free MintoBill demo. We set up your shop, import your products and start your 14-day trial. Already a customer? Find your shop login.',
    crumb: 'Contact',
  },
};

export const routes = Object.keys(pages);

export const canonical = (path) => ORIGIN + (path === '/' ? '/' : path);

const org = {
  '@type': 'Organization',
  '@id': `${ORIGIN}/#organization`,
  name: PLATFORM_NAME,
  url: `${ORIGIN}/`,
  logo: { '@type': 'ImageObject', url: ORIGIN + LOGO, width: 512, height: 512 },
  address: { '@type': 'PostalAddress', addressLocality: contact.city.split(',')[0].trim(), addressRegion: contact.city.split(',')[1]?.trim(), addressCountry: 'IN' },
};

const website = {
  '@type': 'WebSite',
  '@id': `${ORIGIN}/#website`,
  name: PLATFORM_NAME,
  url: `${ORIGIN}/`,
  inLanguage: 'en-IN',
  publisher: { '@id': `${ORIGIN}/#organization` },
};

/** Structured data for one page, as a schema.org @graph. */
export function structuredData(path) {
  const p = pages[path];
  const url = canonical(path);
  const page = {
    '@type': path === '/contact' ? 'ContactPage' : path === '/pricing' && faqs.length ? ['WebPage', 'FAQPage'] : 'WebPage',
    '@id': `${url}#webpage`,
    url,
    name: p.title,
    description: p.description,
    inLanguage: 'en-IN',
    isPartOf: { '@id': `${ORIGIN}/#website` },
    primaryImageOfPage: { '@type': 'ImageObject', url: ORIGIN + OG_IMAGE.path },
  };
  const graph = [org, website, page];

  if (path === '/') {
    // No offers or ratings: prices are not public yet (see Pricing).
    graph.push({
      '@type': 'SoftwareApplication',
      name: PLATFORM_NAME,
      url: `${ORIGIN}/`,
      applicationCategory: 'BusinessApplication',
      applicationSubCategory: 'Point of sale and GST billing',
      operatingSystem: 'Web browser',
      description: p.description,
      publisher: { '@id': `${ORIGIN}/#organization` },
    });
  } else {
    page.breadcrumb = { '@id': `${url}#breadcrumb` };
    graph.push({
      '@type': 'BreadcrumbList',
      '@id': `${url}#breadcrumb`,
      itemListElement: [
        { '@type': 'ListItem', position: 1, name: 'Home', item: `${ORIGIN}/` },
        { '@type': 'ListItem', position: 2, name: p.crumb, item: url },
      ],
    });
  }
  if (path === '/pricing') {
    page.mainEntity = faqs.map(([q, a]) => ({ '@type': 'Question', name: q, acceptedAnswer: { '@type': 'Answer', text: a } }));
  }
  return { '@context': 'https://schema.org', '@graph': graph };
}

const esc = (s) => String(s).replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

/** The <head> tags of one page, as HTML (used by the build). */
export function headTags(path) {
  const p = pages[path];
  const url = canonical(path);
  // JSON inside <script>: escape "<" so the text can never end the element.
  const ld = JSON.stringify(structuredData(path)).replace(/</g, '\\u003c');
  return [
    `<title>${esc(p.title)}</title>`,
    `<meta name="description" content="${esc(p.description)}" />`,
    `<link rel="canonical" href="${esc(url)}" />`,
    `<meta property="og:type" content="website" />`,
    `<meta property="og:site_name" content="${esc(PLATFORM_NAME)}" />`,
    `<meta property="og:locale" content="en_IN" />`,
    `<meta property="og:title" content="${esc(p.title)}" />`,
    `<meta property="og:description" content="${esc(p.description)}" />`,
    `<meta property="og:url" content="${esc(url)}" />`,
    `<meta property="og:image" content="${esc(ORIGIN + OG_IMAGE.path)}" />`,
    `<meta property="og:image:type" content="image/png" />`,
    `<meta property="og:image:width" content="${OG_IMAGE.width}" />`,
    `<meta property="og:image:height" content="${OG_IMAGE.height}" />`,
    `<meta property="og:image:alt" content="${esc(OG_IMAGE.alt)}" />`,
    `<meta name="twitter:card" content="summary_large_image" />`,
    `<meta name="twitter:title" content="${esc(p.title)}" />`,
    `<meta name="twitter:description" content="${esc(p.description)}" />`,
    `<meta name="twitter:image" content="${esc(ORIGIN + OG_IMAGE.path)}" />`,
    `<meta name="twitter:image:alt" content="${esc(OG_IMAGE.alt)}" />`,
    `<script type="application/ld+json">${ld}</script>`,
  ].join('\n    ');
}
