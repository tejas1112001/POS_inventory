import GlobalErrorHandler from './components/GlobalErrorHandler';
import React from 'react';
import { HashRouter as Router, Routes, Route } from 'react-router-dom';
import ProtectedRoute from './components/ProtectedRoute';
import { PosProvider } from './context/PosContext';
import { useError } from './context/ErrorContext';
import Dashboard from './pages/Dashboard';
import Products from './pages/Products';
import AddProduct from './pages/AddProduct';
import Sales from './pages/Sales';
import AddSales from './pages/AddSales';
import Purchases from './pages/Purchases';
import AddPurchase from './pages/AddPurchase';
import Navbar from './components/NavBar';
import POS from './pages/POS';
import Categories from './pages/Categories';
import AddCategory from './pages/AddCategory';
import Brands from './pages/Brands';
import AddBrand from './pages/AddBrand';
import Units from './pages/Units';
import AddUnit from './pages/AddUnit';
import Invoices from './pages/Invoices';
import SalesReturn from './pages/SalesReturn';
import Customers from './pages/Customers';
import AddCustomer from './pages/AddCustomer';
import Billers from './pages/Billers';
import Suppliers from './pages/Suppliers';
import AddSupplier from './pages/AddSupplier';
import PurchaseReport from './pages/PurchaseReport';
import CustomerDueReport from './pages/CustomerDueReport';
import CustomerReport from './pages/CustomerReport';
import SalesReport from './pages/SalesReport';
import SignIn from './pages/SignIn';
import SignUp from './pages/SignUp';
import { useEffect } from 'react';
function App() {
  const { showError } = useError();

  useEffect(() => {
    window.electronAPI?.onBackendError((message) => {
      showError(`Backend failed: ${message}`);
    });
  }, [showError]);

  return (
    <PosProvider>
      <GlobalErrorHandler />
      <Router>
        <div className="flex flex-col h-screen">
          <Navbar className="h-[5%] min-h-[3rem]" />
          <div className="flex-1 overflow-hidden">
            <Routes>
            <Route path="/signin" element={<SignIn />} />
            <Route path="/signup" element={<SignUp />} />
            <Route path="/" element={<ProtectedRoute><Dashboard /></ProtectedRoute>} />
            <Route path="/categories" element={<ProtectedRoute><Categories /></ProtectedRoute>} />
            <Route path="/add-category" element={<ProtectedRoute><AddCategory /></ProtectedRoute>} />
            <Route path="/products" element={<ProtectedRoute><Products /></ProtectedRoute>} />
            <Route path="/brands" element={<ProtectedRoute><Brands /></ProtectedRoute>} />
            <Route path="/add-brand" element={<ProtectedRoute><AddBrand /></ProtectedRoute>} />
            <Route path="/units" element={<ProtectedRoute><Units /></ProtectedRoute>} />
            <Route path="/add-unit" element={<ProtectedRoute><AddUnit /></ProtectedRoute>} />
            <Route path="/invoices" element={<ProtectedRoute><Invoices /></ProtectedRoute>} />
            <Route path="/billers" element={<ProtectedRoute><Billers /></ProtectedRoute>} />
            <Route path="/suppliers" element={<ProtectedRoute><Suppliers /></ProtectedRoute>} />
            <Route path="/add-supplier" element={<ProtectedRoute><AddSupplier /></ProtectedRoute>} />
            <Route path="/sales-return" element={<ProtectedRoute><SalesReturn /></ProtectedRoute>} />
            <Route path="/add-product" element={<ProtectedRoute><AddProduct /></ProtectedRoute>} />
            <Route path="/pos" element={<ProtectedRoute><POS /></ProtectedRoute>} />
            <Route path="/sales" element={<ProtectedRoute><Sales /></ProtectedRoute>} />
            <Route path="/add-sale" element={<ProtectedRoute><AddSales /></ProtectedRoute>} />
            <Route path="/sales-report" element={<ProtectedRoute><SalesReport /></ProtectedRoute>} />
            <Route path="/purchases" element={<ProtectedRoute><Purchases /></ProtectedRoute>} />
            <Route path="/add-purchase" element={<ProtectedRoute><AddPurchase /></ProtectedRoute>} />
            <Route path="/purchase-report" element={<ProtectedRoute><PurchaseReport /></ProtectedRoute>} />
            <Route path="/customer-due-report" element={<ProtectedRoute><CustomerDueReport /></ProtectedRoute>} />
            <Route path="/customers" element={<ProtectedRoute><Customers /></ProtectedRoute>} />
            <Route path="/add-customer" element={<ProtectedRoute><AddCustomer /></ProtectedRoute>} />
            <Route path="/customer-report" element={<ProtectedRoute><CustomerReport /></ProtectedRoute>} />
          </Routes>
          </div>
        </div>
      </Router>
    </PosProvider>
  );
}

export default App;
