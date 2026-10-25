import React, { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import {
  Store,
  MapPin,
  Phone,
  Clock,
  Search,
  Filter,
  ArrowUpDown,
  Tag,
  Sparkles,
  CheckCircle2,
  XCircle,
  ShoppingBag,
  ExternalLink,
  ChevronRight,
  X,
  Info,
  Building2,
  ShieldCheck,
} from 'lucide-react';
import { formatCurrency } from '@/lib/format';

// PublicShopPage is a store's public showcase. Root.jsx passes the store
// slug: the home store at /, any other at /shop/:slug.
export default function PublicShopPage({ slug }) {
  const shopSlug = encodeURIComponent(slug);

  const [shop, setShop] = useState(null);
  const [products, setProducts] = useState([]);
  const [categories, setCategories] = useState([]);
  const [categoryIds, setCategoryIds] = useState({});
  const [featuredProducts, setFeaturedProducts] = useState([]);
  const [selectedCategory, setSelectedCategory] = useState('All');
  const [search, setSearch] = useState('');
  const [sort, setSort] = useState('newest');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  // Selected product modal state
  const [selectedProduct, setSelectedProduct] = useState(null);
  const [, setSelectedVariant] = useState(null);

  useEffect(() => {
    fetchPublicShopData();
  }, [shopSlug]);

  useEffect(() => {
    if (shop?.isPublicEnabled) {
      fetchPublicProducts();
    }
  }, [shopSlug, selectedCategory, search, sort, shop]);

  const fetchPublicShopData = async () => {
    try {
      setLoading(true);
      setError(null);
      const res = await fetch(`/api/public/shop/${shopSlug}`);
      if (!res.ok) {
        throw new Error('Shop showcase unavailable or not found.');
      }
      const data = await res.json();
      setShop(data);

      if (data.isPublicEnabled) {
        // Fetch categories
        const catRes = await fetch(`/api/public/shop/${shopSlug}/categories`);
        if (catRes.ok) {
          const cats = await catRes.json();
          setCategories(['All', ...cats.map((c) => c.name)]);
          setCategoryIds(Object.fromEntries(cats.map((c) => [c.name, c.id])));
        }

        // Fetch featured products
        const featRes = await fetch(`/api/public/shop/${shopSlug}/featured`);
        if (featRes.ok) {
          const feats = await featRes.json();
          setFeaturedProducts(feats || []);
        }
      }
    } catch (err) {
      setError(err.message || 'Failed to connect to shop showcase.');
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  const fetchPublicProducts = async () => {
    try {
      let url = `/api/public/shop/${shopSlug}/products?sort=${sort}`;
      if (search) url += `&search=${encodeURIComponent(search)}`;
      if (selectedCategory && selectedCategory !== 'All') {
        // The API filters by category ID, not by name.
        const id = categoryIds[selectedCategory];
        if (id) url += `&categoryId=${id}`;
      }

      const res = await fetch(url);
      if (res.ok) {
        const data = await res.json();
        setProducts(data.data || []);
      }
    } catch (err) {
      console.error('Failed to load products:', err);
    }
  };

  // The tab shows the shop's name; the staff app's title comes back after.
  useEffect(() => {
    if (!shop?.name) return undefined;
    const before = document.title;
    document.title = shop.name;
    return () => {
      document.title = before;
    };
  }, [shop?.name]);

  const openProductDetails = (product) => {
    setSelectedProduct(product);
    setSelectedVariant(product.variants?.[0] || null);
  };

  if (loading) {
    return (
      <div className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center">
        <div className="flex flex-col items-center gap-3">
          <div className="size-10 rounded-full border-4 border-emerald-500 border-t-transparent animate-spin" />
          <p className="text-sm text-slate-400 font-medium">Loading Shop Catalog...</p>
        </div>
      </div>
    );
  }

  if (error || !shop || !shop.isPublicEnabled) {
    return (
      <div className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center p-4">
        <div className="max-w-md w-full text-center bg-slate-900 border border-slate-800 rounded-2xl p-8 shadow-2xl">
          <Store className="size-12 text-slate-600 mx-auto mb-4" />
          <h2 className="text-xl font-bold mb-2">Showcase Temporarily Offline</h2>
          <p className="text-sm text-slate-400 mb-6">
            {error || 'This shop showcase is currently unavailable or set to offline by the owner.'}
          </p>
          <div className="text-xs text-slate-500 bg-slate-950/60 p-3 rounded-lg border border-slate-800">
            Please visit our physical branch directly for inquiries.
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 font-sans selection:bg-emerald-500/30 selection:text-emerald-300">
      {/* Top Banner Notice */}
      <div className="bg-gradient-to-r from-emerald-600 via-teal-600 to-indigo-600 text-white text-xs py-2 px-4 text-center font-medium shadow-md flex items-center justify-center gap-2">
        <Building2 className="size-3.5" />
        <span>Physical Shop Showcase Only — Visit our store to complete your purchase!</span>
      </div>

      {/* Main Header / Store Hero */}
      <header className="relative border-b border-slate-800 bg-gradient-to-b from-slate-900/90 to-slate-950">
        <div className="max-w-7xl mx-auto px-4 py-8 sm:px-6 lg:px-8">
          <div className="flex flex-col md:flex-row items-start md:items-center justify-between gap-6">
            <div className="flex items-center gap-4">
              <div className="size-16 rounded-2xl bg-gradient-to-br from-emerald-500 to-teal-700 flex items-center justify-center text-white shadow-xl shadow-emerald-500/10 shrink-0 font-bold text-2xl">
                {shop.logo ? (
                  <img src={shop.logo} alt={shop.name} className="size-full rounded-2xl object-cover" />
                ) : (
                  shop.name[0]?.toUpperCase()
                )}
              </div>
              <div>
                <h1 className="text-2xl sm:text-3xl font-extrabold tracking-tight text-white">
                  {shop.name}
                </h1>
                <p className="text-sm text-slate-400 mt-1 max-w-xl">
                  {shop.description || 'Welcome to our product showcase! Browse our live product catalog.'}
                </p>
              </div>
            </div>

            {/* Shop Contact Cards */}
            <div className="flex flex-wrap items-center gap-3 text-xs text-slate-300 w-full md:w-auto">
              {shop.address && (
                <div className="flex items-center gap-2 bg-slate-900 border border-slate-800 rounded-xl px-3 py-2">
                  <MapPin className="size-4 text-emerald-400 shrink-0" />
                  <span>{shop.address}</span>
                </div>
              )}
              {shop.phone && (
                <div className="flex items-center gap-2 bg-slate-900 border border-slate-800 rounded-xl px-3 py-2">
                  <Phone className="size-4 text-teal-400 shrink-0" />
                  <span>{shop.phone}</span>
                </div>
              )}
              {shop.openingHours && (
                <div className="flex items-center gap-2 bg-slate-900 border border-slate-800 rounded-xl px-3 py-2">
                  <Clock className="size-4 text-amber-400 shrink-0" />
                  <span>{shop.openingHours}</span>
                </div>
              )}
            </div>
          </div>
        </div>
      </header>

      {/* Main Content Area */}
      <main className="max-w-7xl mx-auto px-4 py-8 sm:px-6 lg:px-8 space-y-8">
        {/* Featured Section */}
        {featuredProducts.length > 0 && (
          <section className="space-y-4">
            <div className="flex items-center gap-2">
              <Sparkles className="size-5 text-amber-400" />
              <h2 className="text-lg font-bold text-white tracking-tight">Featured Products</h2>
            </div>
            <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-4">
              {featuredProducts.map((p) => (
                <div
                  key={p.id}
                  onClick={() => openProductDetails(p)}
                  className="group relative bg-slate-900/80 border border-amber-500/30 hover:border-amber-500/60 rounded-2xl p-4 transition-all duration-200 cursor-pointer shadow-lg hover:shadow-amber-500/5 hover:-translate-y-1"
                >
                  <div className="absolute top-3 left-3 z-10 bg-amber-500/20 text-amber-300 border border-amber-500/40 text-[10px] font-bold px-2 py-0.5 rounded-full flex items-center gap-1">
                    <Sparkles className="size-3" /> Featured
                  </div>
                  <div className="aspect-square rounded-xl bg-slate-950 overflow-hidden mb-3 flex items-center justify-center border border-slate-800">
                    {p.image ? (
                      <img src={p.image} alt={p.name} className="size-full object-cover group-hover:scale-105 transition-transform duration-300" />
                    ) : (
                      <ShoppingBag className="size-10 text-slate-700" />
                    )}
                  </div>
                  <h3 className="font-semibold text-sm text-slate-100 line-clamp-1 group-hover:text-amber-400 transition-colors">
                    {p.name}
                  </h3>
                  <p className="text-xs text-slate-400 mb-2">{p.category || 'General'}</p>
                  <div className="flex items-center justify-between pt-1 border-t border-slate-800">
                    <span className="text-sm font-bold text-emerald-400">{formatCurrency(p.finalPrice)}</span>
                    <span className={`text-[10px] px-2 py-0.5 rounded-full font-medium ${p.inStock ? 'bg-emerald-500/20 text-emerald-400' : 'bg-red-500/20 text-red-400'}`}>
                      {p.stockStatus}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          </section>
        )}

        {/* Search, Filter & Sort Controls */}
        <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-4 bg-slate-900/60 border border-slate-800/80 rounded-2xl p-4 backdrop-blur-sm">
          {/* Search Bar */}
          <div className="relative flex-1 max-w-md">
            <Search className="absolute left-3 top-3 size-4 text-slate-500" />
            <input
              type="text"
              placeholder="Search products..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full bg-slate-950 border border-slate-800 rounded-xl pl-9 pr-4 py-2 text-sm text-slate-100 placeholder:text-slate-500 focus:outline-none focus:border-emerald-500/50"
            />
          </div>

          {/* Sort Dropdown */}
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-2 text-xs text-slate-400">
              <ArrowUpDown className="size-3.5" />
              <span>Sort:</span>
            </div>
            <select
              value={sort}
              onChange={(e) => setSort(e.target.value)}
              className="bg-slate-950 border border-slate-800 rounded-xl px-3 py-2 text-xs text-slate-200 focus:outline-none"
            >
              <option value="newest">Newest First</option>
              <option value="price_asc">Price: Low to High</option>
              <option value="price_desc">Price: High to Low</option>
              <option value="name_asc">Name: A to Z</option>
            </select>
          </div>
        </div>

        {/* Category Pills */}
        {categories.length > 1 && (
          <div className="flex items-center gap-2 overflow-x-auto pb-2 no-scrollbar">
            {categories.map((cat) => (
              <button
                key={cat}
                type="button"
                onClick={() => setSelectedCategory(cat)}
                className={`px-4 py-2 rounded-xl text-xs font-medium whitespace-nowrap transition-all ${
                  selectedCategory === cat
                    ? 'bg-emerald-500 text-white shadow-lg shadow-emerald-500/20 font-semibold'
                    : 'bg-slate-900 border border-slate-800 text-slate-400 hover:text-slate-200 hover:border-slate-700'
                }`}
              >
                {cat}
              </button>
            ))}
          </div>
        )}

        {/* Product Listing Grid */}
        <section>
          {products.length === 0 ? (
            <div className="text-center py-16 bg-slate-900/40 border border-slate-800/60 rounded-3xl p-8">
              <ShoppingBag className="size-12 text-slate-700 mx-auto mb-3" />
              <h3 className="text-base font-semibold text-slate-300">No Products Available</h3>
              <p className="text-xs text-slate-500 mt-1">
                Try adjusting your category filter or search query.
              </p>
            </div>
          ) : (
            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
              {products.map((p) => (
                <div
                  key={p.id}
                  onClick={() => openProductDetails(p)}
                  className="group bg-slate-900/80 border border-slate-800 hover:border-emerald-500/50 rounded-2xl p-3.5 transition-all duration-200 cursor-pointer shadow-md hover:shadow-emerald-500/5 hover:-translate-y-1 flex flex-col justify-between"
                >
                  <div>
                    <div className="relative aspect-square rounded-xl bg-slate-950 overflow-hidden mb-3 flex items-center justify-center border border-slate-800">
                      {p.hasDiscount && (
                        <span className="absolute top-2 left-2 z-10 bg-rose-500 text-white text-[10px] font-bold px-1.5 py-0.5 rounded-md shadow-sm">
                          {p.discountType === 'Percentage' ? `-${p.discountValue}%` : 'OFF'}
                        </span>
                      )}
                      {p.image ? (
                        <img
                          src={p.image}
                          alt={p.name}
                          className="size-full object-cover group-hover:scale-105 transition-transform duration-300"
                        />
                      ) : (
                        <ShoppingBag className="size-8 text-slate-700" />
                      )}
                    </div>
                    <span className="text-[10px] font-medium text-slate-400 uppercase tracking-wider block mb-0.5">
                      {p.category || 'General'}
                    </span>
                    <h3 className="font-semibold text-xs sm:text-sm text-slate-100 line-clamp-2 group-hover:text-emerald-400 transition-colors leading-tight">
                      {p.name}
                    </h3>
                  </div>

                  <div className="pt-3 mt-3 border-t border-slate-800/80 flex items-end justify-between">
                    <div>
                      {p.hasDiscount && (
                        <span className="text-[10px] text-slate-500 line-through block">
                          {formatCurrency(p.price)}
                        </span>
                      )}
                      <span className="text-xs sm:text-sm font-bold text-emerald-400">
                        {formatCurrency(p.finalPrice)}
                      </span>
                    </div>
                    <span
                      className={`text-[9px] px-2 py-0.5 rounded-full font-medium ${
                        p.inStock
                          ? 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/30'
                          : 'bg-red-500/15 text-red-400 border border-red-500/30'
                      }`}
                    >
                      {p.stockStatus}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>
      </main>

      {/* Product Details Modal / Drawer */}
      {selectedProduct && (
        <div className="fixed inset-0 z-50 bg-slate-950/80 backdrop-blur-md flex items-center justify-center p-4 overflow-y-auto">
          <div className="relative max-w-2xl w-full bg-slate-900 border border-slate-800 rounded-3xl p-6 sm:p-8 shadow-2xl text-slate-100 max-h-[90vh] overflow-y-auto">
            <button
              type="button"
              onClick={() => setSelectedProduct(null)}
              className="absolute top-4 right-4 p-2 rounded-full text-slate-400 hover:text-white hover:bg-slate-800 transition-colors"
            >
              <X className="size-5" />
            </button>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-6">
              {/* Product Image */}
              <div className="aspect-square rounded-2xl bg-slate-950 border border-slate-800 overflow-hidden flex items-center justify-center">
                {selectedProduct.image ? (
                  <img
                    src={selectedProduct.image}
                    alt={selectedProduct.name}
                    className="size-full object-cover"
                  />
                ) : (
                  <ShoppingBag className="size-16 text-slate-700" />
                )}
              </div>

              {/* Info Details */}
              <div className="flex flex-col justify-between space-y-4">
                <div>
                  <div className="flex items-center gap-2 mb-1">
                    <span className="text-xs font-semibold uppercase text-emerald-400 bg-emerald-500/10 px-2.5 py-0.5 rounded-full border border-emerald-500/20">
                      {selectedProduct.category || 'Product Showcase'}
                    </span>
                    {selectedProduct.brand && (
                      <span className="text-xs text-slate-400">Brand: {selectedProduct.brand}</span>
                    )}
                  </div>
                  <h2 className="text-xl font-bold text-white leading-tight mb-2">
                    {selectedProduct.name}
                  </h2>

                  <div className="flex items-baseline gap-3 mb-3">
                    <span className="text-2xl font-extrabold text-emerald-400">
                      {formatCurrency(selectedProduct.finalPrice)}
                    </span>
                    {selectedProduct.hasDiscount && (
                      <span className="text-sm text-slate-500 line-through">
                        {formatCurrency(selectedProduct.price)}
                      </span>
                    )}
                  </div>

                  <div className="flex items-center gap-2 mb-4">
                    {selectedProduct.inStock ? (
                      <span className="inline-flex items-center gap-1.5 text-xs text-emerald-400 bg-emerald-500/15 border border-emerald-500/30 px-3 py-1 rounded-full font-medium">
                        <CheckCircle2 className="size-3.5" /> In Stock at Physical Shop
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1.5 text-xs text-red-400 bg-red-500/15 border border-red-500/30 px-3 py-1 rounded-full font-medium">
                        <XCircle className="size-3.5" /> Out of Stock
                      </span>
                    )}
                  </div>

                  {selectedProduct.description && (
                    <div className="text-xs text-slate-300 bg-slate-950/60 border border-slate-800 p-3 rounded-xl space-y-1">
                      <span className="font-semibold text-slate-400 uppercase text-[10px] block">
                        Description
                      </span>
                      <p className="leading-relaxed">{selectedProduct.description}</p>
                    </div>
                  )}

                  {selectedProduct.warranties && (
                    <div className="text-xs text-amber-300 bg-amber-500/10 border border-amber-500/20 p-2.5 rounded-xl mt-2 flex items-center gap-2">
                      <ShieldCheck className="size-4 shrink-0" />
                      <span>Warranty: {selectedProduct.warranties}</span>
                    </div>
                  )}
                </div>

                {/* Physical Store Action CTA */}
                <div className="space-y-3 pt-4 border-t border-slate-800">
                  <div className="bg-slate-950 border border-slate-800 p-3 rounded-xl text-xs space-y-1 text-slate-300">
                    <div className="flex items-center gap-1.5 text-emerald-400 font-semibold mb-1">
                      <Building2 className="size-3.5" />
                      <span>{shop.name}</span>
                    </div>
                    {shop.address && <p>📍 Address: {shop.address}</p>}
                    {shop.phone && <p>📞 Phone: {shop.phone}</p>}
                  </div>

                  <button
                    type="button"
                    onClick={() => {
                      alert(`To purchase this product, please visit ${shop.name} at:\n${shop.address || 'our physical store location'}`);
                    }}
                    className="w-full bg-gradient-to-r from-emerald-500 to-teal-600 hover:from-emerald-600 hover:to-teal-700 text-white font-bold py-3 px-4 rounded-xl text-sm shadow-lg shadow-emerald-500/20 transition-all flex items-center justify-center gap-2"
                  >
                    <Building2 className="size-4" />
                    Visit Store to Purchase
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Public Footer */}
      <footer className="border-t border-slate-800/80 bg-slate-950 py-8 text-center text-xs text-slate-500 space-y-2">
        <p>© {new Date().getFullYear()} {shop.name}. All rights reserved.</p>
        <p className="text-[11px] text-slate-600">Product Showcase Powered by POS Retail Suite</p>
        <p>
          <Link to="/login" rel="nofollow" className="text-[11px] text-slate-500 hover:text-slate-300 underline-offset-2 hover:underline">
            Staff sign-in
          </Link>
        </p>
      </footer>
    </div>
  );
}
