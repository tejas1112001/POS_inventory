import React, { useState, useEffect } from "react";
import ListPageShell from "../components/layout/ListPageShell";
import PurchaseList from "../components/lists/PurchaseList";
import { useNavigate } from "react-router-dom";
import { api } from "../services/api";

const Purchases = () => {
  const [purchases, setPurchases] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const navigate = useNavigate();

  useEffect(() => {
    fetchPurchases();
  }, []);

  const fetchPurchases = async () => {
    try {
      setLoading(true);
      const data = await api.get('/api/purchases');
      const purchaseRows = Array.isArray(data) ? data : [];
      
      // Format the data to match the frontend expectations
      const formattedData = purchaseRows.map(purchase => ({
        ...purchase,
        paymentStatus: purchase.payment_status, // Map payment_status to paymentStatus
        supplier: purchase.Supplier ? purchase.Supplier.name : 'Unknown Supplier'
      }));
      
      setPurchases(formattedData);
      setError(null);
    } catch (err) {
      setError('Failed to fetch purchases');
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  if (loading) return <div className="p-6">Loading purchases...</div>;
  if (error) return <div className="p-6 text-red-500">{error}</div>;

  return (
    <ListPageShell>
      <PurchaseList
        purchases={purchases || []}
        onAddPurchase={() => navigate("/add-purchase")}
      />
    </ListPageShell>
  );
};

export default Purchases;
