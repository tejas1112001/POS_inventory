// All copy and sample data for the site lives here, so the owner can
// change words, prices and contact details in one place.
// Items in [SQUARE BRACKETS] are placeholders still to be decided.

import {
  BellRing,
  BookOpen,
  CakeSlice,
  CalendarClock,
  ChartColumn,
  Dumbbell,
  Footprints,
  Gift,
  Grid3x3,
  HandCoins,
  Hammer,
  IndianRupee,
  Lock,
  MapPin,
  Package,
  Percent,
  Pill,
  Printer,
  ReceiptText,
  Ruler,
  ScanBarcode,
  Shirt,
  ShoppingBasket,
  Smartphone,
  Sparkles,
  Store,
  Users,
  Zap,
} from 'lucide-react';

export const contact = {
  phone: '9673030057',
  email: 'info@mintobill.com',
  city: 'Pune, Maharashtra',
  callbackHours: '[X]',
};

export const nav = [
  { to: '/', label: 'Home' },
  { to: '/features', label: 'Features' },
  { to: '/industries', label: 'Industries' },
  { to: '/pricing', label: 'Pricing' },
  { to: '/contact', label: 'Contact' },
];

/* ---------------------------------------------------------------- home */

export const heroPoints = [
  '14-day free trial',
  'Works in your browser, with nothing to install',
  'Your own shop address, like yourshop.mintobill.com',
];

export const heroBill = [
  { name: 'Cotton Kurta', meta: 'Size M · Indigo', price: 799, max: 20, icon: Shirt },
  { name: 'Running Shoes', meta: 'UK 9 · Black', price: 2499, max: 5, icon: Footprints },
  { name: 'Paracetamol 500 mg', meta: 'Batch B2291 · Exp 08/27', price: 30, max: 20, icon: Pill },
];

export const trustStrip = [
  { icon: Percent, label: 'GST invoices on every sale' },
  { icon: Package, label: 'Stock that stops overselling' },
  { icon: Lock, label: "Each shop's data kept separate" },
  { icon: MapPin, label: 'Built for Indian retail' },
];

export const bento = {
  checkout: {
    icon: Zap,
    title: 'Fast checkout',
    body: 'Search for a product, pick the size or colour and add it to the bill in seconds. Put a bill on hold while the next customer pays, and take cash, UPI and card on the same bill.',
    tags: ['Keyboard shortcuts', 'Variant picker', 'Held bills', 'Split and part payment'],
  },
  reports: {
    icon: ChartColumn,
    title: 'Reports and Excel export',
    body: 'See sales, purchases and stock on one dashboard. Download any report as an Excel file for your accountant.',
  },
  small: [
    { icon: Percent, title: 'Correct GST on every bill', body: 'Sales within your state get CGST and SGST. Sales to another state get IGST. Discounts come off before tax.' },
    { icon: Package, title: 'Accurate stock', body: 'Stock for each store, a full history of every change, and an alert when an item runs low.' },
    { icon: Printer, title: 'Print, PDF or email', body: 'Print the bill, save it as a PDF or email it to the customer. You can send it again at any time.' },
  ],
  more: [
    { icon: IndianRupee, title: 'Customer dues', body: 'Sell on credit with a limit for each customer, record part payments and share a clear statement.' },
    { icon: Store, title: 'Online catalogue', body: 'Customers can browse your products and prices at your own shop address, without creating an account.' },
    { icon: Users, title: 'Staff and roles', body: 'Separate logins for the owner, admins and cashiers. Each person sees only what their job needs.' },
  ],
};

export const steps = [
  { title: 'We set up your shop', body: 'You get your own address, yourshop.mintobill.com, and logins for you and your staff.' },
  { title: 'Add your products', body: 'Fill in our spreadsheet template and import your whole catalogue in one go.' },
  { title: 'Start billing', body: 'Open the checkout on any computer and make your first GST bill.' },
];

/* ---------------------------------------------------------- industries */

// The tools a shop can switch on. `ready` = available today.
export const tools = {
  general: {
    label: 'Low-stock alerts',
    icon: BellRing,
    ready: true,
    lead: 'Set a reorder level for each product and MintoBill warns you before it runs out.',
    pains: ['Items run out without warning.', 'Reordering is done from memory.'],
    wins: ['Reorder level per product', 'Alerts on the dashboard', 'Purchases and receiving', 'Spreadsheet import'],
    usedBy: ['Every shop'],
  },
  clothing: {
    label: 'Size and colour',
    icon: Grid3x3,
    ready: false,
    lead: 'One style can come in many sizes and colours. See the stock of each combination at a glance.',
    pains: ['Stock of each size and colour is a guess.', 'Customers come back to exchange a size.', 'Price tags are written by hand.'],
    wins: ['Size and colour grid for every style', 'Barcode labels and scanning', 'Exchanges and store credit', 'Your own return rules'],
    usedBy: ['Clothing', 'Sports and fitness', 'Bags and accessories', 'Home textiles'],
  },
  footwear: {
    label: 'Size runs',
    icon: Ruler,
    ready: false,
    lead: 'Popular sizes sell out first. See the stock of every size, for every product, on one screen.',
    pains: ['The popular size runs out while others sit on the shelf.', 'The wrong size is given at the counter.', 'Suppliers send part of an order.'],
    wins: ['Stock by size for each product', 'Scan the box to bill the right size', 'Receive purchases in parts', 'Stock counts'],
    usedBy: ['Footwear', 'Clothing', 'Uniforms', 'Sports and fitness'],
  },
  pharmacy: {
    label: 'Batch and expiry',
    icon: CalendarClock,
    ready: false,
    lead: 'For anything with an expiry date. The batch that expires first is sold first, and expired stock is blocked at the counter.',
    pains: ['Expired stock found on the shelf.', 'Batches are hard to track.', 'Nobody knows what expires next month.'],
    wins: ['Batch and expiry on every purchase', 'Batch with the earliest expiry sold first', 'Expired batches blocked at the counter', 'Expiry alerts'],
    usedBy: ['Pharmacy', 'Grocery and kirana', 'Cosmetics and beauty', 'Bakery and sweets', 'Pet supplies'],
    note: 'For pharmacies: MRP checks, prescriptions and the scheduled-drug register are not part of the first release.',
  },
};

export const toolOrder = ['general', 'clothing', 'footwear', 'pharmacy'];

export const shopTypes = [
  { slug: 'clothing', name: 'Clothing and fashion', icon: Shirt, tint: 'bg-blue-100 text-blue-800', tool: 'clothing' },
  { slug: 'footwear', name: 'Footwear', icon: Footprints, tint: 'bg-indigo-100 text-indigo-800', tool: 'footwear' },
  { slug: 'pharmacy', name: 'Pharmacy and medical', icon: Pill, tint: 'bg-green-100 text-green-800', tool: 'pharmacy' },
  { slug: 'grocery', name: 'Grocery and kirana', icon: ShoppingBasket, tint: 'bg-amber-100 text-amber-800', tool: 'pharmacy' },
  { slug: 'electronics', name: 'Electronics and mobiles', icon: Smartphone, tint: 'bg-slate-200 text-slate-800', tool: 'general' },
  { slug: 'stationery', name: 'Stationery and books', icon: BookOpen, tint: 'bg-pink-100 text-pink-800', tool: 'general' },
  { slug: 'cosmetics', name: 'Cosmetics and beauty', icon: Sparkles, tint: 'bg-fuchsia-100 text-fuchsia-800', tool: 'pharmacy' },
  { slug: 'gifts', name: 'Gifts and toys', icon: Gift, tint: 'bg-orange-100 text-orange-800', tool: 'general' },
  { slug: 'hardware', name: 'Hardware and paints', icon: Hammer, tint: 'bg-stone-200 text-stone-800', tool: 'general' },
  { slug: 'bakery', name: 'Bakery and sweets', icon: CakeSlice, tint: 'bg-yellow-100 text-yellow-800', tool: 'pharmacy' },
  { slug: 'sports', name: 'Sports and fitness', icon: Dumbbell, tint: 'bg-teal-100 text-teal-800', tool: 'clothing' },
];

/* ------------------------------------------------------------ features */

export const featureTabs = [
  { id: 'selling', icon: ScanBarcode, title: 'Fast checkout', sub: 'Bill in seconds', kicker: 'Selling', h: 'Bill a full basket in seconds', body: 'Search or scan, pick the size or colour, and take payment. Put a bill on hold when a customer goes to get one more item.', points: ['Keyboard shortcuts', 'Held bills', 'Split payment', 'Part payment'] },
  { id: 'gst', icon: ReceiptText, title: 'GST bills', sub: 'CGST, SGST and IGST', kicker: 'GST and bills', h: 'The right tax on every bill', body: 'MintoBill works out the tax for you. Sales within your state get CGST and SGST, and sales to another state get IGST.', points: ['Discount before tax', 'Gapless invoice numbers', 'Print, PDF, email'] },
  { id: 'stock', icon: Package, title: 'Stock and history', sub: 'No overselling', kicker: 'Stock', h: 'Know exactly what is on the shelf', body: 'Stock goes down with every sale and up with every purchase. Two counters cannot sell the same last piece.', points: ['Stock per store', 'Full history', 'Low-stock alerts', 'Adjustments with reason'] },
  { id: 'dues', icon: HandCoins, title: 'Customers and dues', sub: 'Credit with limits', kicker: 'Customers', h: 'Give credit without losing track', body: 'Sell on credit to customers you trust, set a limit for each one and record part payments. Their statement is always up to date.', points: ['Credit limits', 'Part payments', 'Statements', 'Exact refunds'] },
  { id: 'reports', icon: ChartColumn, title: 'Reports and Excel', sub: 'See your numbers', kicker: 'Reports', h: 'Your whole shop on one screen', body: 'Sales, purchases and stock in clear reports. Send any of them to your accountant as an Excel file.', points: ['Live dashboard', 'Sales and purchase reports', 'Excel export'] },
  { id: 'shop', icon: Store, title: 'Online catalogue', sub: 'Your shop on the web', kicker: 'Online', h: 'Let customers see your shop online', body: 'Every shop gets its own web address with a catalogue of your products and prices, kept up to date from your stock.', points: ['Own shop address', 'Live prices', 'No login for customers'] },
];

export const sellTiles = [
  { name: 'Cotton Kurta', price: 799, sizes: 'S · M · L · XL', icon: Shirt },
  { name: 'Denim Jeans', price: 1299, sizes: '28 · 30 · 32 · 34', icon: Shirt },
  { name: 'Running Shoes', price: 2499, sizes: 'UK 7 · 8 · 9 · 10', icon: Footprints },
  { name: 'Cotton Socks, pack of 3', price: 249, sizes: 'Free size', icon: Footprints },
  { name: 'Linen Shirt', price: 1099, sizes: 'M · L · XL', icon: Shirt },
  { name: 'Leather Belt', price: 599, sizes: '32 · 34 · 36', icon: Ruler },
];

export const versus = [
  { old: 'GST worked out by hand on each bill', now: 'CGST, SGST or IGST worked out on every bill' },
  { old: 'Stock counted once a month, if at all', now: 'Stock updated with every sale and purchase' },
  { old: 'Bill copies get lost or torn', now: 'Every bill saved, with print, PDF and email' },
  { old: 'Customer dues written in a diary', now: 'Dues, limits and statements per customer' },
  { old: 'Expired stock found on the shelf', now: 'Alerts for low stock and items close to expiry' },
];

/* ------------------------------------------------------------- pricing */

export const plans = [
  { name: 'Starter', forWho: 'For one small shop', items: ['[N] staff logins', '[N] products', 'Email support'] },
  { name: 'Growth', forWho: 'For a busy shop', items: ['[N] staff logins', '[N] products', 'Priority support'], featured: true },
  { name: 'Business', forWho: 'For larger shops', items: ['[N] staff logins', 'Unlimited products', 'Help with setup'] },
];

export const yearlySaving = '[X]%';

export const compareRows = [
  { f: 'Staff logins', v: ['[N]', '[N]', '[N]'] },
  { f: 'Products', v: ['[N]', '[N]', 'Unlimited'] },
  { f: 'GST bills and receipts', v: [true, true, true] },
  { f: 'Stock, purchases and returns', v: [true, true, true] },
  { f: 'Customer credit and dues', v: [true, true, true] },
  { f: 'Reports and Excel export', v: [true, true, true] },
  { f: 'Online catalogue', v: [true, true, true] },
  { f: 'Support', v: ['Email', 'Priority', 'Setup help'] },
];

export const faqs = [
  ['Do I need to install anything?', 'No. MintoBill runs in your web browser. Open your shop address on any computer and log in.'],
  ['Is there a free trial?', 'Yes. Every new shop gets 14 days free, and we set up the shop for you.'],
  ['Does MintoBill take card or UPI payments?', 'No. You take payment the way you do today, and MintoBill records how the customer paid so your totals match.'],
  ['Is my shop data private?', 'Yes. Each shop has its own separate data space. Your staff see only what their role allows.'],
  ['Can customers order online?', 'Not yet. Your online catalogue shows your products and prices, but customers cannot order or pay online.'],
  ['What if I stop paying?', 'Your shop becomes read-only. Nothing is deleted without a warning, and you can still export your data.'],
];

/* ------------------------------------------------------------- contact */

export const bizTypes = ['Clothing', 'Footwear', 'Pharmacy', 'Grocery', 'Other'];

export const demoChecklist = [
  'A list of your products, or an old bill book',
  'Your printer and barcode scanner model, if you have them',
  'Your GSTIN, if your shop has one',
  'Your return rules (exchange, refund, days)',
];
