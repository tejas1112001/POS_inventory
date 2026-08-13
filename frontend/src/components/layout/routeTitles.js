export const routeTitles = {
  '/': 'Dashboard',
  '/pos': 'POS Checkout',
  '/sales': 'Sales',
  '/add-sale': 'Add Sale',
  '/sales-return': 'Sales Returns',
  '/invoices': 'Invoices',
  '/products': 'Products',
  '/add-product': 'Add Product',
  '/inventory': 'Inventory',
  '/purchases': 'Purchases',
  '/add-purchase': 'Add Purchase',
  '/categories': 'Categories',
  '/add-category': 'Add Category',
  '/brands': 'Brands',
  '/add-brand': 'Add Brand',
  '/units': 'Units',
  '/add-unit': 'Add Unit',
  '/suppliers': 'Suppliers',
  '/add-supplier': 'Add Supplier',
  '/customers': 'Customers',
  '/add-customer': 'Add Customer',
  '/billers': 'Billers',
  '/sales-report': 'Sales Report',
  '/purchase-report': 'Purchase Report',
  '/customer-report': 'Customer Report',
  '/customer-due-report': 'Customer Due Report',
  '/settings': 'Settings',
};

export function getRouteTitle(pathname) {
  if (routeTitles[pathname]) return routeTitles[pathname];
  const match = Object.keys(routeTitles).find(
    (route) => route !== '/' && pathname.startsWith(route)
  );
  return match ? routeTitles[match] : 'Dashboard';
}
