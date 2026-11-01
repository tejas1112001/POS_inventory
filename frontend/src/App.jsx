import GlobalErrorHandler from './components/GlobalErrorHandler';
import React, { useEffect } from 'react';
import { Routes, Route, Navigate, useLocation } from 'react-router-dom';
import { ErrorProvider } from './context/ErrorContext';
import ProtectedRoute from './components/ProtectedRoute';
import { PosProvider } from './context/PosContext';
import { PermissionProvider } from './context/PermissionContext';
import { ThemeProvider } from './context/ThemeContext';
import AppLayout from './components/layout/AppLayout';
import { NotFound } from './components/errors/ErrorScreens';
import { ConfirmProvider } from './components/feedback/ConfirmDialog';
import Dashboard from './pages/Dashboard';
import Products from './pages/Products';
import AddProduct from './pages/AddProduct';
import Orders from './pages/Orders';
import OrderDetail from './pages/OrderDetail';
import AddSales from './pages/AddSales';
import Purchases from './pages/Purchases';
import AddPurchase from './pages/AddPurchase';
import CheckoutEntry from './pages/checkout/CheckoutEntry';
import Categories from './pages/Categories';
import Brands from './pages/Brands';
import Units from './pages/Units';
import Invoices from './pages/Invoices';
import SalesReturn from './pages/SalesReturn';
import AddSalesReturn from './pages/AddSalesReturn';
import Customers from './pages/Customers';
import Billers from './pages/Billers';
import Suppliers from './pages/Suppliers';
import PurchaseReport from './pages/PurchaseReport';
import CustomerDueReport from './pages/CustomerDueReport';
import CustomerReport from './pages/CustomerReport';
import SalesReport from './pages/SalesReport';
import Inventory from './pages/Inventory';
import Roles from './pages/Roles';
import AddRole from './pages/AddRole';
import SignIn from './pages/SignIn';
import ForgotPassword from './pages/ForgotPassword';
import SetPassword from './pages/SetPassword';
import Settings from './pages/Settings';
import Billing from './pages/Billing';
import StockAdjustments from './pages/StockAdjustments';
import ShowcaseSettings from './pages/ShowcaseSettings';
import Stores from './pages/Stores';

// The staff app: sign-in and back office. Root.jsx loads it on every path
// that is not the public shop.

function ProtectedLayout({ children, fullBleed = false, module, action = 'view' }) {
  return (
    <ProtectedRoute>
      <AppLayout fullBleed={fullBleed}>
        <ProtectedRoute module={module} action={action}>{children}</ProtectedRoute>
      </AppLayout>
    </ProtectedRoute>
  );
}

// /signin was the sign-in path before /login.
function SignInRedirect() {
  const { search, hash, state } = useLocation();
  return <Navigate to={{ pathname: '/login', search, hash }} state={state} replace />;
}

// Search engines must not list staff pages (the server also sends
// X-Robots-Tag: noindex for them).
function useNoindex() {
  useEffect(() => {
    const meta = document.createElement('meta');
    meta.name = 'robots';
    meta.content = 'noindex';
    document.head.appendChild(meta);
    return () => meta.remove();
  }, []);
}

function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<SignIn />} />
      <Route path="/signin" element={<SignInRedirect />} />
      <Route path="/forgot-password" element={<ForgotPassword />} />
      <Route path="/reset-password" element={<SetPassword purpose="reset" />} />
      <Route path="/accept-invite" element={<SetPassword purpose="invite" />} />
      <Route path="/dashboard" element={<ProtectedLayout module="dashboard"><Dashboard /></ProtectedLayout>} />
      <Route path="/categories" element={<ProtectedLayout module="categories" action="view"><Categories /></ProtectedLayout>} />
      <Route path="/add-category" element={<ProtectedLayout module="categories" action="create"><Categories startNew /></ProtectedLayout>} />
      <Route path="/products" element={<ProtectedLayout module="products" action="view"><Products /></ProtectedLayout>} />
      <Route path="/add-product" element={<ProtectedLayout module="products" action="create"><AddProduct /></ProtectedLayout>} />
      <Route path="/brands" element={<ProtectedLayout module="brands" action="view"><Brands /></ProtectedLayout>} />
      <Route path="/add-brand" element={<ProtectedLayout module="brands" action="create"><Brands startNew /></ProtectedLayout>} />
      <Route path="/units" element={<ProtectedLayout module="units" action="view"><Units /></ProtectedLayout>} />
      <Route path="/add-unit" element={<ProtectedLayout module="units" action="create"><Units startNew /></ProtectedLayout>} />
      <Route path="/invoices" element={<ProtectedLayout module="invoices" action="view"><Invoices /></ProtectedLayout>} />
      <Route path="/billers" element={<ProtectedLayout module="users" action="view"><Billers /></ProtectedLayout>} />
      <Route path="/users" element={<ProtectedLayout module="users" action="view"><Billers /></ProtectedLayout>} />
      <Route path="/roles" element={<ProtectedLayout module="roles" action="view"><Roles /></ProtectedLayout>} />
      <Route path="/add-role" element={<ProtectedLayout module="roles" action="create"><AddRole /></ProtectedLayout>} />
      <Route path="/edit-role/:id" element={<ProtectedLayout module="roles" action="edit"><AddRole /></ProtectedLayout>} />
      <Route path="/suppliers" element={<ProtectedLayout module="suppliers" action="view"><Suppliers /></ProtectedLayout>} />
      <Route path="/add-supplier" element={<ProtectedLayout module="suppliers" action="create"><Suppliers startNew /></ProtectedLayout>} />
      <Route path="/sales-return" element={<ProtectedLayout module="sales_returns" action="view"><SalesReturn /></ProtectedLayout>} />
      <Route path="/add-sales-return" element={<ProtectedLayout module="sales_returns" action="create"><AddSalesReturn /></ProtectedLayout>} />
      <Route path="/pos" element={<ProtectedLayout fullBleed module="pos" action="view"><CheckoutEntry /></ProtectedLayout>} />
      <Route path="/sales" element={<ProtectedLayout module="sales" action="view"><Orders /></ProtectedLayout>} />
      <Route path="/sales/:id" element={<ProtectedLayout module="sales" action="view"><OrderDetail /></ProtectedLayout>} />
      <Route path="/add-sale" element={<ProtectedLayout module="sales" action="create"><AddSales /></ProtectedLayout>} />
      <Route path="/sales-report" element={<ProtectedLayout module="reports" action="view"><SalesReport /></ProtectedLayout>} />
      <Route path="/purchases" element={<ProtectedLayout module="purchases" action="view"><Purchases /></ProtectedLayout>} />
      <Route path="/add-purchase" element={<ProtectedLayout module="purchases" action="create"><AddPurchase /></ProtectedLayout>} />
      <Route path="/purchase-report" element={<ProtectedLayout module="purchases" action="view"><PurchaseReport /></ProtectedLayout>} />
      <Route path="/reports" element={<Navigate to="/sales-report" replace />} />
      <Route path="/customer-due-report" element={<ProtectedLayout module="reports" action="view"><CustomerDueReport /></ProtectedLayout>} />
      <Route path="/customers" element={<ProtectedLayout module="customers" action="view"><Customers /></ProtectedLayout>} />
      <Route path="/add-customer" element={<ProtectedLayout module="customers" action="create"><Customers startNew /></ProtectedLayout>} />
      <Route path="/customer-report" element={<ProtectedLayout module="reports" action="view"><CustomerReport /></ProtectedLayout>} />
      <Route path="/inventory" element={<ProtectedLayout module="inventory" action="view"><Inventory /></ProtectedLayout>} />
      <Route path="/stock-adjustments" element={<ProtectedLayout module="stock_adjustments" action="view"><StockAdjustments /></ProtectedLayout>} />
      <Route path="/settings" element={<ProtectedLayout><Settings /></ProtectedLayout>} />
      <Route path="/billing" element={<ProtectedLayout module="billing" action="view"><Billing /></ProtectedLayout>} />
      <Route path="/stores" element={<ProtectedLayout module="stores" action="view"><Stores /></ProtectedLayout>} />
      <Route path="/showcase-settings" element={<ProtectedLayout module="stores" action="edit"><ShowcaseSettings /></ProtectedLayout>} />
      {/* Unknown addresses: a real "Page not found" inside the app (sign-in first). */}
      <Route path="*" element={<ProtectedLayout><NotFound /></ProtectedLayout>} />
    </Routes>
  );
}

function App() {
  useNoindex();
  return (
    <ErrorProvider>
      <ThemeProvider>
        <PosProvider>
          <PermissionProvider>
            <ConfirmProvider>
              <GlobalErrorHandler />
              <AppRoutes />
            </ConfirmProvider>
          </PermissionProvider>
        </PosProvider>
      </ThemeProvider>
    </ErrorProvider>
  );
}

export default App;
