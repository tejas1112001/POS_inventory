import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '@/components/shared/section-card';
import { api } from '@/services/api';
import { usePos } from '@/hooks/usePos';
import { setShopSettings, useShopSettings } from '@/hooks/useShopSettings';
import { CheckField, Notice, SaveButton, SelectField, TextArea, TextField } from './FormKit';
import { errorNotice } from './formUtils';

const CURRENCIES = ['INR', 'USD', 'EUR', 'GBP', 'AED', 'SAR', 'SGD', 'AUD', 'CAD', 'NZD', 'ZAR', 'KES', 'NGN', 'LKR', 'NPR', 'BDT', 'MYR'];
const LOCALES = [
  ['en-IN', 'India (1,23,456.78)'],
  ['en-US', 'United States (123,456.78)'],
  ['en-GB', 'United Kingdom (123,456.78)'],
  ['en-AE', 'United Arab Emirates'],
  ['en-SG', 'Singapore'],
  ['en-AU', 'Australia'],
  ['en-CA', 'Canada'],
  ['en-NZ', 'New Zealand'],
  ['en-ZA', 'South Africa (123 456,78)'],
  ['en-IE', 'Ireland'],
  ['en-KE', 'Kenya'],
  ['en-NG', 'Nigeria'],
];
const TAX_SPLITS = [
  { value: 'none', label: 'One tax line' },
  { value: 'cgst_sgst', label: 'CGST + SGST (within the state)' },
  { value: 'igst', label: 'IGST (between states)' },
];
const HOME_PAGES = [
  { value: 'login', label: 'Staff login' },
  { value: 'shop', label: 'Online shop' },
];
const LOGO_TYPES = ['image/png', 'image/jpeg', 'image/webp'];
const MAX_LOGO = 2 * 1024 * 1024;

const FIELDS = [
  'businessName', 'legalName', 'address', 'phone', 'email', 'currency', 'locale', 'timezone', 'taxLabel',
  'taxRegistrationNo', 'taxSplit', 'pricesIncludeTax', 'maxDiscountPct', 'invoicePrefix', 'receiptHeader', 'receiptFooter',
  'homePage', 'shopStoreId',
];

function timeZones(current) {
  let list = [];
  try {
    list = Intl.supportedValuesOf('timeZone');
  } catch {
    list = ['Asia/Kolkata', 'UTC'];
  }
  return current && !list.includes(current) ? [current, ...list] : list;
}

function withCurrent(values, current) {
  return current && !values.includes(current) ? [current, ...values] : values;
}

function fromSettings(s) {
  const out = {};
  for (const k of FIELDS) out[k] = s?.[k] ?? (k === 'pricesIncludeTax' ? false : '');
  out.maxDiscountPct = s?.maxDiscountPct != null ? String(s.maxDiscountPct) : '';
  out.homePage = s?.homePage || 'login';
  out.shopStoreId = s?.shopStoreId != null ? String(s.shopStoreId) : '';
  return out;
}

// toBody turns a form value into what PUT /api/settings expects.
function toBody(k, v) {
  if (k === 'maxDiscountPct') return Number(v);
  if (k === 'shopStoreId') return v ? Number(v) : null;
  return v;
}

// storeOptions lists the stores that can be shown at the main address.
// Without stores:view the list is empty and only the current choice shows.
function storeOptions(stores, current) {
  const opts = [{ value: '', label: 'Main store (default)' }];
  for (const st of stores) {
    const notes = [st.status !== 'Active' && 'inactive', !st.isPublicEnabled && 'shop turned off'].filter(Boolean);
    opts.push({ value: String(st.id), label: st.name + (notes.length ? ` (${notes.join(', ')})` : '') });
  }
  if (current && !opts.some((o) => o.value === current)) opts.push({ value: current, label: `Store ${current}` });
  return opts;
}

function readFile(file) {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(r.result);
    r.onerror = () => reject(r.error);
    r.readAsDataURL(file);
  });
}

// ShopSettings edits the business profile, money and time formats, tax
// and the receipt texts (GET/PUT /api/settings). Only changed fields are
// sent. Without settings:edit the form is read-only.
export default function ShopSettings({ canEdit }) {
  const { settings, error: loadError } = useShopSettings();
  const { reloadMe } = usePos();
  const [form, setForm] = useState(() => fromSettings(settings));
  const [logo, setLogo] = useState(null); // data URI waiting to be saved
  const [fields, setFields] = useState({});
  const [notice, setNotice] = useState(null);
  const [busy, setBusy] = useState(false);
  const [stores, setStores] = useState([]);

  useEffect(() => {
    if (settings) setForm(fromSettings(settings));
  }, [settings]);

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

  const zones = useMemo(() => timeZones(form.timezone), [form.timezone]);
  const set = (k) => (v) => setForm((f) => ({ ...f, [k]: v }));
  const dis = !canEdit || busy;

  const pickLogo = async (e) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    if (!LOGO_TYPES.includes(file.type)) {
      setNotice({ kind: 'error', text: 'Use a PNG, JPEG or WebP image.' });
      return;
    }
    if (file.size > MAX_LOGO) {
      setNotice({ kind: 'error', text: 'The logo can be at most 2 MB.' });
      return;
    }
    setLogo(await readFile(file));
    setNotice(null);
  };

  const save = async (e) => {
    e.preventDefault();
    const original = fromSettings(settings);
    const body = {};
    for (const k of FIELDS) {
      if (form[k] !== original[k]) body[k] = toBody(k, form[k]);
    }
    if (logo) body.logo = logo;
    if (Object.keys(body).length === 0) {
      setNotice({ kind: 'ok', text: 'Nothing to save.' });
      return;
    }
    setBusy(true);
    setNotice(null);
    setFields({});
    try {
      const saved = await api.put('/api/settings', body, { quiet: true });
      setShopSettings(saved);
      setLogo(null);
      if ('currency' in body || 'locale' in body || 'timezone' in body || 'businessName' in body) await reloadMe();
      setNotice({ kind: 'ok', text: 'Settings saved.' });
    } catch (err) {
      setFields(err.fields || {});
      setNotice(errorNotice(err));
    } finally {
      setBusy(false);
    }
  };

  if (!settings) {
    return <SectionCard title="Shop">{loadError ? <Notice notice={errorNotice(loadError)} /> : <p className="text-sm text-muted-foreground">Loading...</p>}</SectionCard>;
  }

  return (
    <form onSubmit={save} className="space-y-3" noValidate>
      <SectionCard title="Business profile">
        <div className="grid gap-4 md:grid-cols-2">
          <TextField id="businessName" label="Business name" value={form.businessName} onChange={set('businessName')} error={fields.businessName} disabled={dis} maxLength={120} required />
          <TextField id="legalName" label="Legal name" hint="If different, shown on invoices." value={form.legalName} onChange={set('legalName')} error={fields.legalName} disabled={dis} maxLength={200} />
          <TextArea id="address" label="Address" value={form.address} onChange={set('address')} error={fields.address} disabled={dis} maxLength={500} />
          <div className="space-y-4">
            <TextField id="phone" label="Phone" value={form.phone} onChange={set('phone')} error={fields.phone} disabled={dis} maxLength={40} />
            <TextField id="email" type="email" label="Email" value={form.email} onChange={set('email')} error={fields.email} disabled={dis} />
          </div>
          <div className="space-y-1.5 md:col-span-2">
            <span className="block text-sm font-medium">Logo</span>
            <div className="flex items-center gap-3">
              {logo || settings.logo ? (
                <img src={logo || settings.logo} alt="Shop logo" className="size-14 rounded-md border object-contain" />
              ) : (
                <div className="flex size-14 items-center justify-center rounded-md border text-xs text-muted-foreground">None</div>
              )}
              {canEdit && (
                <label className="cursor-pointer rounded-md border px-3 py-1.5 text-sm hover:bg-accent">
                  Choose image
                  <input type="file" accept={LOGO_TYPES.join(',')} className="sr-only" onChange={pickLogo} disabled={dis} />
                </label>
              )}
              {logo && <span className="text-xs text-muted-foreground">Save to use the new logo.</span>}
            </div>
            {fields.logo && <p className="text-xs text-destructive">{fields.logo}</p>}
          </div>
        </div>
      </SectionCard>

      <SectionCard title="Money and time">
        <div className="grid gap-4 md:grid-cols-3">
          <SelectField id="currency" label="Currency" value={form.currency} onChange={set('currency')} error={fields.currency} disabled={dis}
            options={withCurrent(CURRENCIES, form.currency).map((c) => ({ value: c, label: c }))} />
          <SelectField id="locale" label="Number format" value={form.locale} onChange={set('locale')} error={fields.locale} disabled={dis}
            options={withCurrent(LOCALES.map((l) => l[0]), form.locale).map((v) => ({ value: v, label: LOCALES.find((l) => l[0] === v)?.[1] || v }))} />
          <SelectField id="timezone" label="Time zone" value={form.timezone} onChange={set('timezone')} error={fields.timezone} disabled={dis}
            options={zones.map((z) => ({ value: z, label: z.replace(/_/g, ' ') }))} />
        </div>
        <p className="mt-3 text-xs text-muted-foreground">The app stays in English; this only changes how amounts and dates look. Dates in reports follow the time zone.</p>
      </SectionCard>

      <SectionCard title="Tax and invoices">
        <div className="grid gap-4 md:grid-cols-3">
          <TextField id="taxLabel" label="Tax name" hint="For example GST or VAT." value={form.taxLabel} onChange={set('taxLabel')} error={fields.taxLabel} disabled={dis} maxLength={20} />
          <TextField id="taxRegistrationNo" label="Tax registration number" hint="GSTIN or VAT number. Receipts become tax invoices." value={form.taxRegistrationNo} onChange={set('taxRegistrationNo')} error={fields.taxRegistrationNo} disabled={dis} maxLength={30} />
          <SelectField id="taxSplit" label="Tax lines on receipts" value={form.taxSplit} onChange={set('taxSplit')} error={fields.taxSplit} disabled={dis} options={TAX_SPLITS} />
          <TextField id="invoicePrefix" label="Invoice number prefix" hint="Letters, digits and - only." value={form.invoicePrefix} onChange={set('invoicePrefix')} error={fields.invoicePrefix} disabled={dis} maxLength={20} />
          <TextField id="maxDiscountPct" type="number" min="0" max="100" step="0.01" label="Largest discount without approval (%)" value={form.maxDiscountPct} onChange={set('maxDiscountPct')} error={fields.maxDiscountPct} disabled={dis} />
          <div className="pt-6">
            <CheckField id="pricesIncludeTax" label="Prices include tax" hint="Used for products without their own setting." checked={form.pricesIncludeTax} onChange={set('pricesIncludeTax')} disabled={dis} />
          </div>
        </div>
      </SectionCard>

      <SectionCard title="Receipt text">
        <div className="grid gap-4 md:grid-cols-2">
          <TextArea id="receiptHeader" label="Header" hint="Shown above the shop name." value={form.receiptHeader} onChange={set('receiptHeader')} error={fields.receiptHeader} disabled={dis} maxLength={500} />
          <TextArea id="receiptFooter" label="Footer" hint="For example a return policy or a thank-you note." value={form.receiptFooter} onChange={set('receiptFooter')} error={fields.receiptFooter} disabled={dis} maxLength={500} />
        </div>
      </SectionCard>

      <SectionCard title="Home page">
        <div className="grid gap-4 md:grid-cols-2">
          <SelectField id="homePage" label={`What ${window.location.host}/ shows`} hint="Staff always sign in at /login." value={form.homePage} onChange={set('homePage')} error={fields.homePage} disabled={dis} options={HOME_PAGES} />
          {form.homePage === 'shop' && (
            <SelectField id="shopStoreId" label="Store shown" hint="Its shop moves from /shop/... to the main address; the old link still works." value={form.shopStoreId} onChange={set('shopStoreId')} error={fields.shopStoreId} disabled={dis} options={storeOptions(stores, form.shopStoreId)} />
          )}
        </div>
        {form.homePage === 'shop' && (
          <p className="mt-3 text-xs text-muted-foreground">
            If that store is inactive or its shop is turned off, or your plan has no online shop, the main address shows the staff login.
          </p>
        )}
      </SectionCard>

      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0 flex-1">
          <Notice notice={notice} />
        </div>
        {canEdit ? <SaveButton busy={busy} /> : <p className="text-sm text-muted-foreground">You can view these settings but not change them.</p>}
      </div>
    </form>
  );
}
