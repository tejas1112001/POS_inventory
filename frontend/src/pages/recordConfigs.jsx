// Page settings for the RecordsPage lists (categories, brands, units,
// customers, suppliers): columns, form fields and how a form becomes the
// request body. Field names match the server's.
import { Wallet } from 'lucide-react';
import { formatCurrency } from '@/lib/format';
import { AuthImage } from '@/components/shared/auth-image';
import StatusBadge from '@/components/kit/StatusBadge';
import { ImageField, SelectField, STATUS_OPTIONS, TextAreaField, TextField } from '@/components/kit/Form';

const trim = (v) => (typeof v === 'string' ? v.trim() : v);

function NameCell({ row }) {
  const initial = (row.name || '?').charAt(0).toUpperCase();
  return (
    <span className="flex items-center gap-2">
      <AuthImage
        src={row.image}
        alt=""
        className="size-8 shrink-0 rounded-md object-cover ring-1 ring-border"
        fallback={<span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-xs font-medium text-muted-foreground ring-1 ring-border">{initial}</span>}
      />
      <span className="truncate">{row.name}</span>
    </span>
  );
}

const name = { id: 'name', header: 'Name', card: 'title', cell: (r) => <NameCell row={r} /> };
const status = { id: 'status', header: 'Status', card: 'meta', cell: (r) => <StatusBadge value={r.status} /> };
const createdBy = { id: 'createdBy', header: 'Created by', card: 'hidden', className: 'hidden lg:table-cell', cell: (r) => r.createdBy || '—' };
const dash = (v) => v || '—';

// image: undefined keeps the picture; '' removes it; a data URL replaces it.
const withImage = (body, values) => (values.image === undefined ? body : { ...body, image: values.image });

const named = (noun, nounPlural, endpoint, module) => ({
  endpoint,
  module,
  noun,
  nounPlural,
  columns: [name, status, createdBy],
  defaults: (r) => ({ name: r.name || '', status: r.status || 'Active', image: undefined }),
  payload: (v) => withImage({ name: trim(v.name), status: v.status }, v),
  fields: (r) => (
    <>
      <TextField name="name" label={`${noun} name`} required maxLength={120} autoFocus />
      <SelectField name="status" label="Status" options={STATUS_OPTIONS} />
      <ImageField name="image" label="Picture (optional)" current={r.image} />
    </>
  ),
});

export const categoryConfig = { ...named('Category', 'Categories', '/api/categories', 'categories'), description: 'Group products for the till and the online shop.' };
export const brandConfig = { ...named('Brand', 'Brands', '/api/brands', 'brands'), description: 'Brands shown on products and in reports.' };

export const unitConfig = {
  endpoint: '/api/units',
  module: 'units',
  noun: 'Unit',
  nounPlural: 'Units',
  description: 'How products are counted (piece, pack, box).',
  columns: [
    { id: 'name', header: 'Name', card: 'title', cell: (r) => r.name },
    { id: 'short', header: 'Short name', card: 'subtitle', cell: (r) => dash(r.short_name) },
    status,
    createdBy,
  ],
  defaults: (r) => ({ name: r.name || '', short_name: r.short_name || '', status: r.status || 'Active' }),
  payload: (v) => ({ name: trim(v.name), short_name: trim(v.short_name), status: v.status }),
  fields: () => (
    <>
      <TextField name="name" label="Unit name" required maxLength={60} autoFocus />
      <TextField name="short_name" label="Short name" maxLength={20} hint="For example pc, pk, kg." />
      <SelectField name="status" label="Status" options={STATUS_OPTIONS} />
    </>
  ),
};

const contactFields = (withCity) =>
  function fields(r) {
    return (
      <>
        <TextField name="name" label="Name" required maxLength={120} autoFocus />
        <TextField name="phone" label="Phone" type="tel" maxLength={40} autoComplete="off" />
        <TextField name="email" label="Email" type="email" maxLength={254} autoComplete="off" rules={{ pattern: { value: /^[^\s@]+@[^\s@]+\.[^\s@]+$/, message: 'Enter a valid email address.' } }} />
        <SelectField name="status" label="Status" options={STATUS_OPTIONS} />
        <TextAreaField name="address" label="Address" rows={2} />
        {withCity && <TextField name="city" label="City" maxLength={120} />}
        {withCity && <TextField name="country" label="Country" maxLength={120} />}
        <ImageField name="image" label="Picture (optional)" current={r.image} />
      </>
    );
  };

export const customerConfig = {
  endpoint: '/api/customers',
  module: 'customers',
  noun: 'Customer',
  nounPlural: 'Customers',
  description: 'People you sell to. Their email is used for receipts.',
  searchHint: 'Search name, phone or email…',
  columns: [
    name,
    { id: 'phone', header: 'Phone', card: 'subtitle', cell: (r) => dash(r.phone) },
    { id: 'email', header: 'Email', card: 'meta', className: 'hidden md:table-cell', cell: (r) => dash(r.email) },
    { id: 'city', header: 'City', card: 'hidden', className: 'hidden lg:table-cell', cell: (r) => dash(r.city) },
    // S10 item 5: what the customer owes now, and their credit limit.
    {
      id: 'due',
      header: 'Owes',
      card: 'meta',
      className: 'text-right tabular-nums',
      cell: (r) => (Number(r.due) > 0 ? <span className="font-medium text-danger-fg">{formatCurrency(r.due)}</span> : '—'),
    },
    { id: 'creditLimit', header: 'Credit limit', card: 'hidden', className: 'hidden lg:table-cell text-right tabular-nums', cell: (r) => (r.creditLimit == null ? 'No credit' : formatCurrency(r.creditLimit)) },
    status,
  ],
  // The customer's account: dues, payments, statement, credit limit.
  extraActions: (row, navigate) => [{ label: 'Account and dues', icon: Wallet, onSelect: () => navigate(`/customers/${row.id}`) }],
  defaults: (r) => ({ name: r.name || '', phone: r.phone || '', email: r.email || '', address: r.address || '', city: r.city || '', country: r.country || '', status: r.status || 'Active', image: undefined }),
  payload: (v) => withImage({ name: trim(v.name), phone: trim(v.phone), email: trim(v.email), address: trim(v.address), city: trim(v.city), country: trim(v.country), status: v.status }, v),
  fields: contactFields(true),
};

export const supplierConfig = {
  endpoint: '/api/suppliers',
  module: 'suppliers',
  noun: 'Supplier',
  nounPlural: 'Suppliers',
  description: 'Who you buy stock from.',
  searchHint: 'Search name, phone or email…',
  columns: [
    name,
    { id: 'phone', header: 'Phone', card: 'subtitle', cell: (r) => dash(r.phone) },
    { id: 'email', header: 'Email', card: 'meta', className: 'hidden md:table-cell', cell: (r) => dash(r.email) },
    status,
  ],
  defaults: (r) => ({ name: r.name || '', phone: r.phone || '', email: r.email || '', address: r.address || '', status: r.status || 'Active', image: undefined }),
  payload: (v) => withImage({ name: trim(v.name), phone: trim(v.phone), email: trim(v.email), address: trim(v.address), status: v.status }, v),
  fields: contactFields(false),
};
