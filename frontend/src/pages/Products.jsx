// Products.jsx
import React, { useState, useEffect } from 'react';
import ListPageShell from '../components/layout/ListPageShell';
import ProductList from '../components/lists/ProductList';
import { NavLink } from 'react-router-dom';
import ImportProduct from "../components/ImportProduct";
import { api } from '../services/api';

export default function Products() {
  const [products, setProducts] = useState([]);
  const [showForm, setShowForm] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    fetchProducts();
  }, []);

  const fetchProducts = async () => {
    try {
      setLoading(true);
      setError(null);
      const data = await api.get('/api/products');
      setProducts(Array.isArray(data) ? data : []);
    } catch (err) {
      setError('Failed to fetch products');
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  if (loading) return <div className="py-10 text-center text-muted-foreground">Loading products...</div>;
  if (error) return <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-destructive">{error}</div>;

  return (
    <ListPageShell>
      <ProductList
        products={products || []}
        setShowForm={setShowForm}
        onRefresh={fetchProducts}
      />
      {showForm && (
        <ImportProduct
          showForm={showForm}
          setShowForm={setShowForm}
          onImportComplete={fetchProducts}
        />
      )}
    </ListPageShell>
  );
}
