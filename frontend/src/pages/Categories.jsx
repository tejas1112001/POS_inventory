import React, { useState, useEffect } from 'react';
import ListPageShell from '../components/layout/ListPageShell';
import CategoryList from '../components/lists/CategoryList';
import { useNavigate } from 'react-router-dom';
import { api } from '../services/api';
import { formatDate } from '@/lib/format';

export default function Categories() {
  const [categories, setCategories] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const navigate = useNavigate();

  useEffect(() => {
    fetchCategories();
  }, []);

  const fetchCategories = async () => {
    try {
      setLoading(true);
      const data = await api.get('/api/categories');
      const categoryRows = Array.isArray(data) ? data : [];

      // Format dates for display
      const formattedData = categoryRows.map(category => ({
        ...category,
        created_at: formatDate(category.created_at)
      }));
      
      setCategories(formattedData);
      setError(null);
    } catch (err) {
      setError('Failed to fetch categories');
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  if (loading) return <div className="p-6">Loading categories...</div>;
  if (error) return <div className="p-6 text-red-500">{error}</div>;

  return (
    <ListPageShell>
      <CategoryList
        categories={categories || []}
        setShowForm={() => navigate('/add-category')}
        onRefresh={fetchCategories}
      />
    </ListPageShell>
  );
}
