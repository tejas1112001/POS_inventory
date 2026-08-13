import GlobalErrorHandler from './components/GlobalErrorHandler';
import React, { useEffect } from 'react';
import { HashRouter as Router, Routes, Route } from 'react-router-dom';
import ProtectedRoute from './components/ProtectedRoute';
import { PosProvider } from './context/PosContext';
import { ThemeProvider } from './context/ThemeContext';
import { useError } from './context/ErrorContext';
import AppLayout from './components/layout/AppLayout';
import Dashboard from './pages/Dashboard';
import Products from './pages/Products';
import AddProduct from './pages/AddProduct';
import Sales from './pages/Sales';
import AddSales from './pages/AddSales';
import Purchases from './pages/Purchases';
import AddPurchase from './pages/AddPurchase';
import POS from './pages/POS';
import Categories from './pages/Categories';
import AddCategory from './pages/AddCategory';
import Brands from './pages/Brands';
import AddBrand from './pages/AddBrand';
import Units from './pages/Units';
import AddUnit from './pages/AddUnit';
import Invoices from './pages/Invoices';
import SalesReturn from './pages/SalesReturn';
import AddSalesReturn from './pages/AddSalesReturn';
import Customers from './pages/Customers';
import AddCustomer from './pages/AddCustomer';
import Billers from './pages/Billers';
import Suppliers from './pages/Suppliers';
import AddSupplier from './pages/AddSupplier';
import PurchaseReport from './pages/PurchaseReport';
import CustomerDueReport from './pages/CustomerDueReport';
import CustomerReport from './pages/CustomerReport';
import SalesReport from './pages/SalesReport';
import Inventory from './pages/Inventory';
import SignIn from './pages/SignIn';
import SignUp from './pages/SignUp';
import Settings from './pages/Settings';

function ProtectedLayout({ children, fullBleed = false }) {
  return (
    <ProtectedRoute>
      <AppLayout fullBleed={fullBleed}>{children}</AppLayout>
    </ProtectedRoute>
  );
}

function AppRoutes() {
  const { showError } = useError();

  useEffect(() => {
    window.electronAPI?.onBackendError((message) => {
      showError(`Backend failed: ${message}`);
    });
  }, [showError]);

  return (
    <Routes>
      <Route path="/signin" element={<SignIn />} />
      <Route path="/signup" element={<SignUp />} />
      <Route path="/" element={<ProtectedLayout><Dashboard /></ProtectedLayout>} />
      <Route path="/categories" element={<ProtectedLayout><Categories /></ProtectedLayout>} />
      <Route path="/add-category" element={<ProtectedLayout><AddCategory /></ProtectedLayout>} />
      <Route path="/products" element={<ProtectedLayout><Products /></ProtectedLayout>} />
      <Route path="/brands" element={<ProtectedLayout><Brands /></ProtectedLayout>} />
      <Route path="/add-brand" element={<ProtectedLayout><AddBrand /></ProtectedLayout>} />
      <Route path="/units" element={<ProtectedLayout><Units /></ProtectedLayout>} />
      <Route path="/add-unit" element={<ProtectedLayout><AddUnit /></ProtectedLayout>} />
      <Route path="/invoices" element={<ProtectedLayout><Invoices /></ProtectedLayout>} />
      <Route path="/billers" element={<ProtectedLayout><Billers /></ProtectedLayout>} />
      <Route path="/suppliers" element={<ProtectedLayout><Suppliers /></ProtectedLayout>} />
      <Route path="/add-supplier" element={<ProtectedLayout><AddSupplier /></ProtectedLayout>} />
      <Route path="/sales-return" element={<ProtectedLayout><SalesReturn /></ProtectedLayout>} />
      <Route path="/add-sales-return" element={<ProtectedLayout><AddSalesReturn /></ProtectedLayout>} />
      <Route path="/add-product" element={<ProtectedLayout><AddProduct /></ProtectedLayout>} />
      <Route path="/pos" element={<ProtectedLayout fullBleed><POS /></ProtectedLayout>} />
      <Route path="/sales" element={<ProtectedLayout><Sales /></ProtectedLayout>} />
      <Route path="/add-sale" element={<ProtectedLayout><AddSales /></ProtectedLayout>} />
      <Route path="/sales-report" element={<ProtectedLayout><SalesReport /></ProtectedLayout>} />
      <Route path="/purchases" element={<ProtectedLayout><Purchases /></ProtectedLayout>} />
      <Route path="/add-purchase" element={<ProtectedLayout><AddPurchase /></ProtectedLayout>} />
      <Route path="/purchase-report" element={<ProtectedLayout><PurchaseReport /></ProtectedLayout>} />
      <Route path="/customer-due-report" element={<ProtectedLayout><CustomerDueReport /></ProtectedLayout>} />
      <Route path="/customers" element={<ProtectedLayout><Customers /></ProtectedLayout>} />
      <Route path="/add-customer" element={<ProtectedLayout><AddCustomer /></ProtectedLayout>} />
      <Route path="/customer-report" element={<ProtectedLayout><CustomerReport /></ProtectedLayout>} />
      <Route path="/inventory" element={<ProtectedLayout><Inventory /></ProtectedLayout>} />
      <Route path="/settings" element={<ProtectedLayout><Settings /></ProtectedLayout>} />
    </Routes>
  );
}

function App() {
  return (
    <ThemeProvider>
      <PosProvider>
        <GlobalErrorHandler />
        <Router>
          <AppRoutes />
        </Router>
      </PosProvider>
    </ThemeProvider>
  );
}

export default App;
