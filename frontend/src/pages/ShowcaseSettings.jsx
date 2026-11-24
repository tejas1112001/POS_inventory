import { ProductImage } from '@/components/shared/product-image';
import { useConfirm } from '../components/feedback/ConfirmDialog';
import { toast } from '../../shared/toast/store';
import React, { useState, useEffect } from 'react';
import { Check, Copy, ExternalLink, Eye, EyeOff, Save, Sparkles, Star, Store as StoreIcon } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import FormField, { formTextareaClass } from '@/components/forms/FormField';
import DataTable from '@/components/kit/DataTable';
import FilterBar from '@/components/kit/FilterBar';
import IconTile from '@/components/kit/IconTile';
import PageHeader from '@/components/kit/PageHeader';
import Panel from '@/components/kit/Panel';
import TogglePill from '@/components/kit/TogglePill';
import { EmptyState, LoadingRows } from '@/components/kit/States';
import { formatCurrency } from '@/lib/format';
import { api } from '@/services/api';
import { usePos } from '@/hooks/usePos';
import { usePermission } from '@/context/PermissionContext';

// Store slugs: lowercase letters, digits and dashes (the server's rule).
const toSlug = (v) => v.toLowerCase().replace(/[^a-z0-9-]/g, '');

export default function ShowcaseSettings() {
  const { currentStore } = usePos();
  const confirm = useConfirm();
  const { hasPermission } = usePermission();
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
  const [statusFilter, setStatusFilter] = useState('');

  // Every store has its own showcase; pick which one to manage.
  const [stores, setStores] = useState([]);
  const [pickedStoreId, setPickedStoreId] = useState(null);
  const selectedStoreId = pickedStoreId || currentStore?.id || stores[0]?.id || 1;
  // What the server has: links use the saved slug, not the one being typed.
  const [saved, setSaved] = useState({ slug: '', isPublicEnabled: false, status: '' });
  // isDefault: this is the default public store; homePage "shop" puts it at /.
  const [home, setHome] = useState({ isDefault: false, homePage: 'login' });
  const [settingDefault, setSettingDefault] = useState(false);

  useEffect(() => {
    fetchShowcaseData();
  }, [selectedStoreId]);

  useEffect(() => {
    let live = true;
    api
      .get('/api/stores', { quiet: true })
      .then((list) => live && setStores(Array.isArray(list) ? list : []))
      .catch(() => {});
    return () => {
      live = false;
    };
  }, []);

  const fetchShowcaseData = async () => {
    try {
      setLoading(true);
      const res = await api.get(`/api/stores/${selectedStoreId}/showcase`);
      if (res.store) {
        setStoreData({
          name: res.store.name || '',
          slug: res.store.slug,
          description: res.store.description || '',
          phone: res.store.phone || '',
          address: res.store.address || '',
          openingHours: res.store.openingHours || '',
          logo: res.store.logo || '',
          banner: res.store.banner || '',
          isPublicEnabled: res.store.isPublicEnabled ?? true,
        });
        setSaved({ slug: res.store.slug, isPublicEnabled: !!res.store.isPublicEnabled, status: res.store.status });
        setHome({ isDefault: !!res.isDefault, homePage: res.homePage || 'login' });
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
    // A new web address breaks links people already have: ask first.
    if (saved.slug && storeData.slug !== saved.slug) {
      const ok = await confirm({
        title: 'Change the web address?',
        body: `From /shop/${saved.slug} to /shop/${storeData.slug}. Links and QR codes that use the old address stop working.`,
        confirmLabel: 'Change address',
        destructive: true,
      });
      if (!ok) return;
    }
    try {
      setSaving(true);
      await api.put(`/api/stores/${selectedStoreId}/showcase`, storeData, { quiet: true });
      toast.success('Online shop details saved.');
      fetchShowcaseData();
    } catch (err) {
      toast.error(err.message || 'The details could not be saved.');
    } finally {
      setSaving(false);
    }
  };

  const handleSetDefault = async () => {
    try {
      setSettingDefault(true);
      await api.post(`/api/stores/${selectedStoreId}/showcase/default`, undefined, { quiet: true });
      await fetchShowcaseData();
      toast.success('This store is now the default online shop.');
    } catch (err) {
      toast.error(err.message || 'The default store could not be changed.');
    } finally {
      setSettingDefault(false);
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

  // The default store's shop is at / when the home page shows the shop.
  const atHome = home.isDefault && home.homePage === 'shop';
  const publicUrl = atHome ? `${window.location.origin}/` : `${window.location.origin}/shop/${saved.slug}`;
  const canBeDefault = saved.status === 'Active' && saved.isPublicEnabled;

  const copyLink = () => {
    navigator.clipboard.writeText(publicUrl);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const filteredProducts = products.filter((p) => {
    const matchesSearch =
      p.name.toLowerCase().includes(search.toLowerCase()) ||
      (p.category || '').toLowerCase().includes(search.toLowerCase());
    if (statusFilter === 'Published') return matchesSearch && p.isPublished;
    if (statusFilter === 'Unpublished') return matchesSearch && !p.isPublished;
    if (statusFilter === 'Featured') return matchesSearch && p.isFeatured;
    return matchesSearch;
  });

  const SHOW = [
    { value: 'Published', label: 'Published' },
    { value: 'Unpublished', label: 'Hidden' },
    { value: 'Featured', label: 'Featured' },
  ];
  const columns = [
    {
      id: 'name',
      header: 'Product',
      card: 'title',
      cell: (p) => (
        <span className="flex items-center gap-3 font-medium text-navy">
          <ProductImage name={p.name} src={p.image} size={32} rounded="rounded-lg" />
          {p.name}
        </span>
      ),
    },
    { id: 'category', header: 'Category', card: 'subtitle', cell: (p) => <span className="text-muted-foreground">{p.category || 'Unassigned'}</span> },
    { id: 'price', header: 'Price', card: 'meta', align: 'right', cell: (p) => <span className="font-semibold">{formatCurrency(p.price)}</span> },
    {
      id: 'published',
      header: 'Online',
      card: 'meta',
      cell: (p) => (
        <TogglePill
          on={p.isPublished}
          onClick={() => handleToggleProduct(p.id, 'isPublished', p.isPublished)}
          onLabel="Published"
          offLabel="Hidden"
          icon={Eye}
          offIcon={EyeOff}
          label={`Show ${p.name} in the online shop`}
        />
      ),
    },
    {
      id: 'featured',
      header: 'Featured',
      card: 'meta',
      cell: (p) => (
        <TogglePill
          on={p.isFeatured}
          onClick={() => handleToggleProduct(p.id, 'isFeatured', p.isFeatured)}
          onLabel="Featured"
          offLabel="Standard"
          icon={Sparkles}
          tone="amber"
          label={`Feature ${p.name}`}
        />
      ),
    },
  ];
  const slugHint = atHome
    ? `Default store: ${window.location.origin}/ (also /shop/${saved.slug}). Lowercase letters, digits and dashes.`
    : `${window.location.origin}/shop/${storeData.slug || 'your-shop'} · lowercase letters, digits and dashes.`;

  return (
    <div className="flex flex-col gap-3">
      <PageHeader
        actions={
          <>
            {stores.length > 1 && (
              <select
                aria-label="Store"
                value={String(selectedStoreId)}
                onChange={(e) => setPickedStoreId(Number(e.target.value))}
                className="h-10 rounded-lg border border-input bg-card px-3 text-sm shadow-xs"
              >
                {stores.map((st) => (
                  <option key={st.id} value={String(st.id)}>
                    {st.name}
                  </option>
                ))}
              </select>
            )}
            <Button variant="outline" onClick={copyLink}>
              {copied ? <Check className="size-4" aria-hidden="true" /> : <Copy className="size-4" aria-hidden="true" />}
              {copied ? 'Link copied' : 'Copy link'}
            </Button>
            <Button asChild>
              <a href={publicUrl} target="_blank" rel="noopener noreferrer">
                <ExternalLink className="size-4" aria-hidden="true" /> Preview shop
              </a>
            </Button>
          </>
        }
      />

      {loading ? (
        <div className="rounded-2xl border bg-card shadow-xs">
          <LoadingRows rows={4} />
        </div>
      ) : (
        <>
          <Panel title="Status" id="shop-status">
            <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
              <div className="flex min-w-0 items-center gap-3">
                <IconTile icon={StoreIcon} tone={storeData.isPublicEnabled ? 'teal' : 'muted'} />
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-semibold text-navy">{storeData.name || 'Online shop'}</span>
                    <span
                      className={
                        storeData.isPublicEnabled
                          ? 'rounded-md bg-teal-soft px-2 py-0.5 text-xs font-semibold text-primary dark:text-teal'
                          : 'rounded-md bg-muted px-2 py-0.5 text-xs font-semibold text-muted-foreground'
                      }
                    >
                      {storeData.isPublicEnabled ? 'Live' : 'Offline'}
                    </span>
                    {home.isDefault && (
                      <span className="inline-flex items-center gap-1 rounded-md bg-amber-soft px-2 py-0.5 text-xs font-semibold text-[#7a4a05] dark:text-amber-brand">
                        <Star className="size-3" aria-hidden="true" /> Default store
                      </span>
                    )}
                  </div>
                  <p className="truncate font-mono text-xs text-muted-foreground select-all">{publicUrl}</p>
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-3">
                <Switch checked={storeData.isPublicEnabled} onCheckedChange={(checked) => setStoreData({ ...storeData, isPublicEnabled: checked })} label="Show the online shop" />
                {!home.isDefault && hasPermission('stores', 'edit') && (
                  <Button
                    variant="outline"
                    onClick={handleSetDefault}
                    disabled={!canBeDefault || settingDefault}
                    title={canBeDefault ? undefined : 'Turn the online shop on and save first.'}
                  >
                    <Star className="size-4" aria-hidden="true" />
                    {settingDefault ? 'Saving…' : 'Make default store'}
                  </Button>
                )}
              </div>
            </div>
            {home.isDefault && home.homePage !== 'shop' && (
              <p className="mt-2 text-xs text-muted-foreground">The main address shows the staff sign-in. To show this shop there, choose Online shop in Settings, Home page.</p>
            )}
          </Panel>

          <Panel title="Shop details" id="shop-details">
            <form onSubmit={handleSaveProfile} className="space-y-3">
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                <FormField label="Web address" htmlFor="sc-slug" hint={slugHint}>
                  <Input
                    id="sc-slug"
                    value={storeData.slug}
                    onChange={(e) => setStoreData({ ...storeData, slug: toSlug(e.target.value) })}
                    placeholder="abc-fashion"
                    maxLength={60}
                    autoCapitalize="none"
                    spellCheck={false}
                  />
                </FormField>
                <FormField label="Phone" htmlFor="sc-phone">
                  <Input id="sc-phone" value={storeData.phone} onChange={(e) => setStoreData({ ...storeData, phone: e.target.value })} placeholder="+91 98765 43210" inputMode="tel" />
                </FormField>
                <FormField label="Address" htmlFor="sc-address">
                  <Input id="sc-address" value={storeData.address} onChange={(e) => setStoreData({ ...storeData, address: e.target.value })} placeholder="Shop 12, Main Market" />
                </FormField>
                <FormField label="Opening hours" htmlFor="sc-hours">
                  <Input id="sc-hours" value={storeData.openingHours} onChange={(e) => setStoreData({ ...storeData, openingHours: e.target.value })} placeholder="Mon–Sat 10 am – 9 pm" />
                </FormField>
                <FormField label="Welcome note" htmlFor="sc-desc" className="md:col-span-2">
                  <textarea
                    id="sc-desc"
                    rows={3}
                    value={storeData.description}
                    onChange={(e) => setStoreData({ ...storeData, description: e.target.value })}
                    className={formTextareaClass}
                    placeholder="Browse what we have in store today."
                  />
                </FormField>
              </div>
              <div className="flex justify-end">
                <Button type="submit" disabled={saving}>
                  <Save className="size-4" aria-hidden="true" />
                  {saving ? 'Saving…' : 'Save details'}
                </Button>
              </div>
            </form>
          </Panel>

          <section aria-labelledby="shop-products" className="flex flex-col">
            <h2 id="shop-products" className="mb-2 text-base font-bold text-navy">
              Products in the online shop
            </h2>
            <FilterBar
              search={search}
              onSearch={setSearch}
              placeholder="Search products…"
              filters={[{ key: 'show', label: 'Show', allLabel: 'All products', options: SHOW }]}
              values={{ show: statusFilter }}
              onFilter={(_, v) => setStatusFilter(v)}
              onClear={() => {
                setSearch('');
                setStatusFilter('');
              }}
              actions={
                <>
                  <Button variant="outline" onClick={() => handleBatchAction(true)}>
                    <Eye className="size-4" aria-hidden="true" /> Publish all
                  </Button>
                  <Button variant="outline" onClick={() => handleBatchAction(false)}>
                    <EyeOff className="size-4" aria-hidden="true" /> Hide all
                  </Button>
                </>
              }
            />
            <DataTable
              label="Online shop products"
              columns={columns}
              rows={filteredProducts}
              empty={<EmptyState title="No matching products">Try another search or filter.</EmptyState>}
            />
          </section>
        </>
      )}
    </div>
  );
}
