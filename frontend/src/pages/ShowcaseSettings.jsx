import React, { useState, useEffect } from 'react';
import {
  Globe,
  Store as StoreIcon,
  Check,
  Copy,
  ExternalLink,
  QrCode,
  Sparkles,
  Eye,
  EyeOff,
  Search,
  Filter,
  Save,
  RefreshCw,
} from 'lucide-react';
import { SectionCard } from '@/components/shared/section-card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { formatCurrency } from '@/lib/format';
import { api } from '@/services/api';
import { usePos } from '@/hooks/usePos';

export default function ShowcaseSettings() {
  const { currentStore } = usePos();
  const [storeData, setStoreData] = useState({
    name: '',
    slug: '',
    description: '',
    phone: '',
    address: '',
    openingHours: '',
    logo: '',
    banner: '',
    isPublicEnabled: true,
  });
  const [products, setProducts] = useState([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [copied, setCopied] = useState(false);
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState('All');
  const [showQR, setShowQR] = useState(false);

  const selectedStoreId = currentStore?.id || 1;

  useEffect(() => {
    fetchShowcaseData();
  }, [selectedStoreId]);

  const fetchShowcaseData = async () => {
    try {
      setLoading(true);
      const res = await api.get(`/api/stores/${selectedStoreId}/showcase`);
      if (res.store) {
        setStoreData({
          name: res.store.name || '',
          slug: res.store.slug || `store-${res.store.id}`,
          description: res.store.description || '',
          phone: res.store.phone || '',
          address: res.store.address || '',
          openingHours: res.store.openingHours || '',
          logo: res.store.logo || '',
          banner: res.store.banner || '',
          isPublicEnabled: res.store.isPublicEnabled ?? true,
        });
      }
      setProducts(res.products || []);
    } catch (err) {
      console.error('Failed to load showcase data:', err);
    } finally {
      setLoading(false);
    }
  };

  const handleSaveProfile = async (e) => {
    e.preventDefault();
    try {
      setSaving(true);
      await api.put(`/api/stores/${selectedStoreId}/showcase`, storeData);
      alert('Shop Showcase settings saved successfully!');
      fetchShowcaseData();
    } catch (err) {
      alert(err.message || 'Failed to save settings');
    } finally {
      setSaving(false);
    }
  };

  const handleToggleProduct = async (productId, field, currentValue) => {
    try {
      const newValue = !currentValue;
      setProducts((prev) =>
        prev.map((p) => (p.id === productId ? { ...p, [field]: newValue } : p))
      );
      await api.post(`/api/stores/${selectedStoreId}/showcase/toggle-product`, {
        productId,
        field,
        value: newValue,
      });
    } catch (err) {
      console.error('Failed to update product showcase setting:', err);
      fetchShowcaseData();
    }
  };

  const handleBatchAction = async (publishAll) => {
    try {
      setProducts((prev) => prev.map((p) => ({ ...p, isPublished: publishAll })));
      await api.post(`/api/stores/${selectedStoreId}/showcase/batch-publish`, { publishAll });
    } catch (err) {
      console.error('Batch publish error:', err);
      fetchShowcaseData();
    }
  };

  const publicUrl = `${window.location.origin}/#/shop/${storeData.slug || selectedStoreId}`;

  const copyLink = () => {
    navigator.clipboard.writeText(publicUrl);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const filteredProducts = products.filter((p) => {
    const matchesSearch =
      p.name.toLowerCase().includes(search.toLowerCase()) ||
      p.category.toLowerCase().includes(search.toLowerCase());
    if (statusFilter === 'Published') return matchesSearch && p.isPublished;
    if (statusFilter === 'Unpublished') return matchesSearch && !p.isPublished;
    if (statusFilter === 'Featured') return matchesSearch && p.isFeatured;
    return matchesSearch;
  });

  if (loading) {
    return <div className="p-8 text-center text-muted-foreground">Loading Showcase Settings...</div>;
  }

  return (
    <div className="flex h-full flex-col gap-6 p-4 sm:p-6 max-w-7xl mx-auto overflow-y-auto">
      {/* Header */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between border-b pb-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight flex items-center gap-2">
            <Globe className="size-6 text-primary" />
            Product Showcase Settings
          </h1>
          <p className="text-sm text-muted-foreground">
            Manage your public shop profile and control which products are visible to physical store customers.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={copyLink} className="gap-2">
            {copied ? <Check className="size-4 text-emerald-500" /> : <Copy className="size-4" />}
            {copied ? 'Copied Link' : 'Copy Public Link'}
          </Button>
          <a href={publicUrl} target="_blank" rel="noopener noreferrer">
            <Button size="sm" className="gap-2">
              <ExternalLink className="size-4" />
              Preview Public Shop
            </Button>
          </a>
        </div>
      </div>

      {/* Global Status & Share Banner */}
      <div className="rounded-xl border bg-card p-5 shadow-sm flex flex-col md:flex-row items-start md:items-center justify-between gap-4">
        <div className="flex items-center gap-4">
          <div
            className={`flex size-12 items-center justify-center rounded-xl ${
              storeData.isPublicEnabled
                ? 'bg-emerald-500/10 text-emerald-600 dark:bg-emerald-500/20'
                : 'bg-muted text-muted-foreground'
            }`}
          >
            <StoreIcon className="size-6" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h3 className="font-semibold text-lg">{storeData.name || 'Store Showcase'}</h3>
              <span
                className={`text-xs px-2.5 py-0.5 rounded-full font-medium ${
                  storeData.isPublicEnabled
                    ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
                    : 'bg-muted text-muted-foreground'
                }`}
              >
                {storeData.isPublicEnabled ? 'Public Showcase Active' : 'Catalog Offline'}
              </span>
            </div>
            <p className="text-xs text-muted-foreground mt-0.5 font-mono select-all">
              {publicUrl}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-3 w-full md:w-auto">
          <Switch
            checked={storeData.isPublicEnabled}
            onCheckedChange={(checked) => setStoreData({ ...storeData, isPublicEnabled: checked })}
            label="Enable Public Showcase"
          />
        </div>
      </div>

      {/* Profile Settings */}
      <SectionCard title="Shop Profile Details">
        <form onSubmit={handleSaveProfile} className="space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className="text-xs font-semibold uppercase text-muted-foreground mb-1 block">
                Public URL Slug
              </label>
              <Input
                value={storeData.slug}
                onChange={(e) => setStoreData({ ...storeData, slug: e.target.value })}
                placeholder="e.g. abc-fashion"
              />
              <p className="text-[11px] text-muted-foreground mt-1">
                Your store link will be: {window.location.origin}/#/shop/
                <span className="font-semibold text-foreground">{storeData.slug || 'slug'}</span>
              </p>
            </div>
            <div>
              <label className="text-xs font-semibold uppercase text-muted-foreground mb-1 block">
                Contact Phone
              </label>
              <Input
                value={storeData.phone}
                onChange={(e) => setStoreData({ ...storeData, phone: e.target.value })}
                placeholder="+92 300 1234567"
              />
            </div>
            <div>
              <label className="text-xs font-semibold uppercase text-muted-foreground mb-1 block">
                Physical Shop Address
              </label>
              <Input
                value={storeData.address}
                onChange={(e) => setStoreData({ ...storeData, address: e.target.value })}
                placeholder="Shop 12, Main Market, Islamabad"
              />
            </div>
            <div>
              <label className="text-xs font-semibold uppercase text-muted-foreground mb-1 block">
                Opening Hours
              </label>
              <Input
                value={storeData.openingHours}
                onChange={(e) => setStoreData({ ...storeData, openingHours: e.target.value })}
                placeholder="Mon - Sat: 10:00 AM - 9:00 PM, Sun: Closed"
              />
            </div>
            <div className="md:col-span-2">
              <label className="text-xs font-semibold uppercase text-muted-foreground mb-1 block">
                Shop Description / Welcome Note
              </label>
              <textarea
                rows={3}
                value={storeData.description}
                onChange={(e) => setStoreData({ ...storeData, description: e.target.value })}
                className="w-full rounded-md border border-input bg-background p-2.5 text-sm focus:outline-none focus:ring-2 focus:ring-primary"
                placeholder="Welcome to our store! Browse our live product catalog and visit our physical branch to make your purchase."
              />
            </div>
          </div>
          <div className="flex justify-end pt-2">
            <Button type="submit" disabled={saving} className="gap-2">
              <Save className="size-4" />
              {saving ? 'Saving...' : 'Save Profile Changes'}
            </Button>
          </div>
        </form>
      </SectionCard>

      {/* Product Publishing Manager */}
      <SectionCard title="Product Publishing Control">
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 mb-4">
          <div className="flex items-center gap-2 flex-1 max-w-md">
            <div className="relative w-full">
              <Search className="absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
              <Input
                placeholder="Search products to publish..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="pl-9"
              />
            </div>
            <select
              value={statusFilter}
              onChange={(e) => setStatusFilter(e.target.value)}
              className="rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none"
            >
              <option value="All">All Products</option>
              <option value="Published">Published Only</option>
              <option value="Unpublished">Unpublished Only</option>
              <option value="Featured">Featured Only</option>
            </select>
          </div>

          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleBatchAction(true)}
              className="gap-1.5 text-xs"
            >
              <Eye className="size-3.5" />
              Publish All
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => handleBatchAction(false)}
              className="gap-1.5 text-xs text-muted-foreground"
            >
              <EyeOff className="size-3.5" />
              Unpublish All
            </Button>
          </div>
        </div>

        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full text-left text-sm">
            <thead className="bg-muted/50 border-b text-xs uppercase font-semibold text-muted-foreground">
              <tr>
                <th className="p-3">Product Name</th>
                <th className="p-3">Category</th>
                <th className="p-3">POS Price</th>
                <th className="p-3 text-center">Public Status</th>
                <th className="p-3 text-center">Featured Badge</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {filteredProducts.length === 0 ? (
                <tr>
                  <td colSpan={5} className="p-6 text-center text-muted-foreground">
                    No matching products found.
                  </td>
                </tr>
              ) : (
                filteredProducts.map((p) => (
                  <tr key={p.id} className="hover:bg-muted/30 transition-colors">
                    <td className="p-3 font-medium flex items-center gap-3">
                      {p.image ? (
                        <img
                          src={p.image}
                          alt={p.name}
                          className="size-8 rounded object-cover border"
                        />
                      ) : (
                        <div className="size-8 rounded bg-muted flex items-center justify-center text-xs text-muted-foreground font-bold">
                          {p.name[0]}
                        </div>
                      )}
                      <span>{p.name}</span>
                    </td>
                    <td className="p-3 text-muted-foreground">{p.category || 'Unassigned'}</td>
                    <td className="p-3 font-semibold">{formatCurrency(p.price)}</td>
                    <td className="p-3 text-center">
                      <button
                        type="button"
                        onClick={() => handleToggleProduct(p.id, 'isPublished', p.isPublished)}
                        className={`inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium transition-colors ${
                          p.isPublished
                            ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400 hover:bg-emerald-500/25'
                            : 'bg-muted text-muted-foreground hover:bg-muted/80'
                        }`}
                      >
                        {p.isPublished ? <Eye className="size-3.5" /> : <EyeOff className="size-3.5" />}
                        {p.isPublished ? 'Published' : 'Unpublished'}
                      </button>
                    </td>
                    <td className="p-3 text-center">
                      <button
                        type="button"
                        onClick={() => handleToggleProduct(p.id, 'isFeatured', p.isFeatured)}
                        className={`inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium transition-colors ${
                          p.isFeatured
                            ? 'bg-amber-500/15 text-amber-700 dark:text-amber-400 hover:bg-amber-500/25'
                            : 'bg-muted text-muted-foreground hover:bg-muted/80'
                        }`}
                      >
                        <Sparkles className="size-3.5" />
                        {p.isFeatured ? 'Featured' : 'Standard'}
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </SectionCard>
    </div>
  );
}
