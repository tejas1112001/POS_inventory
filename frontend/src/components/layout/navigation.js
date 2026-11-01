// The staff app's menu (docs/UI_REDESIGN_PLAN.md §4.2). Each item names the
// permission that shows it; a group with no visible item is hidden. These
// only decide what is shown: the server checks every action itself.
import {
  BarChart3,
  Boxes,
  CreditCard,
  FileText,
  Globe,
  LayoutDashboard,
  Package,
  ReceiptText,
  RotateCcw,
  Ruler,
  Settings,
  ShieldCheck,
  ShoppingCart,
  SlidersHorizontal,
  Award,
  Tag,
  Truck,
  UserCog,
  Users,
  Building2,
} from 'lucide-react';

export const REPORT_PATHS = ['/reports', '/sales-report', '/purchase-report', '/customer-report', '/customer-due-report'];

export const NAV = [
  { title: 'Overview', items: [{ label: 'Dashboard', path: '/dashboard', icon: LayoutDashboard, perm: ['dashboard', 'view'] }] },
  {
    title: 'Sell',
    items: [
      { label: 'Checkout', path: '/pos', icon: ShoppingCart, perm: ['pos', 'view'] },
      { label: 'Orders', path: '/sales', icon: ReceiptText, perm: ['sales', 'view'], also: ['/add-sale'] },
      { label: 'Returns', path: '/sales-return', icon: RotateCcw, perm: ['sales_returns', 'view'], also: ['/add-sales-return'] },
      { label: 'Invoices & dues', path: '/invoices', icon: FileText, perm: ['invoices', 'view'] },
    ],
  },
  {
    title: 'Inventory',
    items: [
      { label: 'Products', path: '/products', icon: Package, perm: ['products', 'view'], also: ['/add-product'] },
      { label: 'Stock', path: '/inventory', icon: Boxes, perm: ['inventory', 'view'] },
      { label: 'Adjustments & history', path: '/stock-adjustments', icon: SlidersHorizontal, perm: ['stock_adjustments', 'view'] },
      { label: 'Purchases', path: '/purchases', icon: Truck, perm: ['purchases', 'view'], also: ['/add-purchase'] },
    ],
  },
  {
    title: 'Contacts',
    items: [
      { label: 'Customers', path: '/customers', icon: Users, perm: ['customers', 'view'], also: ['/add-customer'] },
      { label: 'Suppliers', path: '/suppliers', icon: Building2, perm: ['suppliers', 'view'], also: ['/add-supplier'] },
    ],
  },
  {
    title: 'Catalogue setup',
    items: [
      { label: 'Categories', path: '/categories', icon: Tag, perm: ['categories', 'view'], also: ['/add-category'] },
      { label: 'Brands', path: '/brands', icon: Award, perm: ['brands', 'view'], also: ['/add-brand'] },
      { label: 'Units', path: '/units', icon: Ruler, perm: ['units', 'view'], also: ['/add-unit'] },
    ],
  },
  { title: 'Reports', items: [{ label: 'Reports', path: '/sales-report', icon: BarChart3, perm: ['reports', 'view'], also: REPORT_PATHS }] },
  { title: 'Online shop', items: [{ label: 'Storefront', path: '/showcase-settings', icon: Globe, perm: ['stores', 'edit'] }] },
  {
    title: 'Team',
    items: [
      { label: 'Users', path: '/users', icon: UserCog, perm: ['users', 'view'], also: ['/billers'] },
      { label: 'Roles & permissions', path: '/roles', icon: ShieldCheck, perm: ['roles', 'view'], also: ['/add-role', '/edit-role'] },
    ],
  },
  {
    title: 'Settings',
    items: [
      { label: 'Settings', path: '/settings', icon: Settings, perm: null },
      { label: 'Plan & billing', path: '/billing', icon: CreditCard, perm: ['billing', 'view'] },
    ],
  },
];

// visibleNav keeps the items the user may open and drops empty groups.
export function visibleNav(hasPermission) {
  return NAV.map((g) => ({ ...g, items: g.items.filter((i) => !i.perm || hasPermission(i.perm[0], i.perm[1])) })).filter(
    (g) => g.items.length > 0
  );
}

// isItemActive: the item's page or one of its sub-pages is open.
export function isItemActive(item, pathname) {
  return [item.path, ...(item.also || [])].some((p) => pathname === p || pathname.startsWith(`${p}/`));
}

// Page titles for the top bar (new names; old addresses keep working).
export const TITLES = {
  '/dashboard': 'Dashboard',
  '/pos': 'Checkout',
  '/sales': 'Orders',
  '/add-sale': 'New order',
  '/sales-return': 'Returns',
  '/add-sales-return': 'New return',
  '/invoices': 'Invoices & dues',
  '/products': 'Products',
  '/add-product': 'Add product',
  '/inventory': 'Stock',
  '/stock-adjustments': 'Adjustments & history',
  '/purchases': 'Purchases',
  '/add-purchase': 'New purchase',
  '/categories': 'Categories',
  '/add-category': 'Add category',
  '/brands': 'Brands',
  '/add-brand': 'Add brand',
  '/units': 'Units',
  '/add-unit': 'Add unit',
  '/suppliers': 'Suppliers',
  '/add-supplier': 'Add supplier',
  '/customers': 'Customers',
  '/add-customer': 'Add customer',
  '/users': 'Users',
  '/billers': 'Users',
  '/roles': 'Roles & permissions',
  '/add-role': 'New role',
  '/edit-role': 'Edit role',
  '/reports': 'Reports',
  '/sales-report': 'Reports',
  '/purchase-report': 'Reports',
  '/customer-report': 'Reports',
  '/customer-due-report': 'Reports',
  '/showcase-settings': 'Storefront',
  '/settings': 'Settings',
  '/billing': 'Plan & billing',
};

export function pageTitle(pathname) {
  if (TITLES[pathname]) return TITLES[pathname];
  const match = Object.keys(TITLES).find((p) => pathname.startsWith(`${p}/`));
  return match ? TITLES[match] : '';
}
