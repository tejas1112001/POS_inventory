import { useCallback, useEffect, useState } from 'react';
import { Loader2, Pencil, Plus, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { useConfirm } from '@/components/feedback/ConfirmDialog';
import PageHeader from '@/components/kit/PageHeader';
import StatusBadge from '@/components/kit/StatusBadge';
import { EmptyState, ErrorState, LoadingRows } from '@/components/kit/States';
import { usePermission } from '@/context/PermissionContext';
import { api } from '@/services/api';
import { toast } from '../../shared/toast/store';

const inputCls =
  'h-11 w-full rounded-lg border border-input bg-card px-3 text-base outline-none focus-visible:border-primary focus-visible:ring-[3px] focus-visible:ring-ring/25 aria-[invalid=true]:border-danger sm:h-9 sm:text-sm';

const LEVELS = { product: 'Product (one value per product, e.g. Season)', variant: 'Variant (a dimension, e.g. Size)' };
const KINDS = { choice: 'Choose from a list', text: 'Free text' };

const empty = { key: '', label: '', level: 'variant', kind: 'choice', choicesText: '', sort: 0, status: 'Active' };

// Attributes (industry P1 item 4, A6): the shop's attribute definitions.
// A choice attribute lists its values (one per line); a choice in use by
// a product cannot be removed or renamed (the server says how many use it).
export default function Attributes() {
  const { hasPermission } = usePermission();
  const confirm = useConfirm();
  const [rows, setRows] = useState(null);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState(null); // {id?, ...form}
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState('');
  const [fields, setFields] = useState({});

  const load = useCallback(() => {
    setError('');
    api
      .get('/api/attributes', { quiet: true })
      .then((d) => setRows(d || []))
      .catch((err) => setError(err.message || 'Could not load the attributes.'));
  }, []);
  useEffect(load, [load]);

  const open = (d) => {
    setFormError('');
    setFields({});
    setEditing(d ? { ...d, choicesText: (d.choices || []).join('\n') } : { ...empty });
  };
  const set = (k) => (e) => setEditing((cur) => ({ ...cur, [k]: e.target.value }));

  const save = async (e) => {
    e.preventDefault();
    setBusy(true);
    setFormError('');
    setFields({});
    const body = {
      label: editing.label,
      choices: editing.kind === 'choice' ? editing.choicesText.split('\n').map((s) => s.trim()).filter(Boolean) : [],
      sort: Number(editing.sort) || 0,
      status: editing.status,
    };
    try {
      if (editing.id) {
        await api.put(`/api/attributes/${editing.id}`, body, { quiet: true });
      } else {
        await api.post('/api/attributes', { ...body, key: editing.key, level: editing.level, kind: editing.kind }, { quiet: true });
      }
      toast.success('Attribute saved.');
      setEditing(null);
      load();
    } catch (err) {
      setFields(err.fields || {});
      setFormError(err.message || 'Could not save the attribute.');
    } finally {
      setBusy(false);
    }
  };

  const remove = async (d) => {
    const ok = await confirm({
      title: `Delete "${d.label}"?`,
      body: d.products ? `${d.products} products use it, so it cannot be deleted. Set it to Inactive instead.` : 'No product uses it.',
      confirmLabel: 'Delete',
      destructive: true,
    });
    if (!ok) return;
    try {
      await api.delete(`/api/attributes/${d.id}`, { quiet: true });
      toast.success('Attribute deleted.');
      load();
    } catch (err) {
      toast.error(err.message || 'The attribute could not be deleted.');
    }
  };

  const canEdit = hasPermission('attributes', 'edit');
  return (
    <div>
      <PageHeader
        actions={
          hasPermission('attributes', 'create') && (
            <Button type="button" onClick={() => open(null)} className="max-sm:h-11">
              <Plus className="size-4" aria-hidden="true" /> New attribute
            </Button>
          )
        }
      />
      {error && <ErrorState message={error} onRetry={load} />}
      {!error && !rows && <LoadingRows />}
      {rows && rows.length === 0 && <EmptyState title="No attributes yet">Add Size and Colour to build size and colour variants.</EmptyState>}
      {rows && rows.length > 0 && (
        <div className="overflow-x-auto rounded-2xl border bg-card shadow-xs">
          <table className="w-full text-sm">
            <thead className="bg-muted text-left text-xs uppercase tracking-wide text-slate-600 dark:text-slate-300">
              <tr>
                <th scope="col" className="px-4 py-2 font-medium">Attribute</th>
                <th scope="col" className="px-3 py-2 font-medium">Used as</th>
                <th scope="col" className="px-3 py-2 font-medium">Values</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Products</th>
                <th scope="col" className="px-3 py-2 font-medium">Status</th>
                <th scope="col" className="px-3 py-2"><span className="sr-only">Actions</span></th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {rows.map((d) => (
                <tr key={d.id}>
                  <td className="px-4 py-2">
                    <div className="font-medium">{d.label}</div>
                    <div className="text-xs text-muted-foreground">{d.key}</div>
                  </td>
                  <td className="px-3 py-2">{d.level === 'variant' ? 'Variant dimension' : 'Product detail'}</td>
                  <td className="max-w-xs px-3 py-2 text-muted-foreground">
                    {d.kind === 'text' ? 'Free text' : d.choices.slice(0, 12).join(', ') + (d.choices.length > 12 ? ` … (${d.choices.length})` : '')}
                  </td>
                  <td className="px-3 py-2 text-right tabular-nums">{d.products}</td>
                  <td className="px-3 py-2">
                    <StatusBadge value={d.status} />
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-right">
                    {canEdit && (
                      <Button type="button" variant="ghost" size="icon" aria-label={`Edit ${d.label}`} onClick={() => open(d)}>
                        <Pencil className="size-4" aria-hidden="true" />
                      </Button>
                    )}
                    {hasPermission('attributes', 'delete') && (
                      <Button type="button" variant="ghost" size="icon" aria-label={`Delete ${d.label}`} onClick={() => remove(d)}>
                        <Trash2 className="size-4" aria-hidden="true" />
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Dialog open={!!editing} onOpenChange={(o) => !o && setEditing(null)}>
        <DialogContent sheet className="sm:max-w-lg">
          {editing && (
            <form onSubmit={save} noValidate className="grid gap-4">
              <DialogHeader>
                <DialogTitle>{editing.id ? `Edit ${editing.label}` : 'New attribute'}</DialogTitle>
                <DialogDescription>
                  {editing.id ? 'The key, use and kind are fixed. A value in use by a product cannot be removed or renamed.' : 'For example Size (variant, choose from a list) or Colour (variant, free text).'}
                </DialogDescription>
              </DialogHeader>
              {formError && <InlineAlert>{formError}</InlineAlert>}
              <label className="grid gap-1.5 text-sm font-medium">
                Label
                <input className={inputCls} value={editing.label} onChange={set('label')} aria-invalid={!!fields.label} maxLength={60} required />
              </label>
              {!editing.id && (
                <>
                  <label className="grid gap-1.5 text-sm font-medium">
                    Key <span className="text-xs font-normal text-muted-foreground">Optional: made from the label (size, size_system). Used in SKU patterns as {'{key}'}.</span>
                    <input className={inputCls} value={editing.key} onChange={set('key')} aria-invalid={!!fields.key} maxLength={40} />
                  </label>
                  <label className="grid gap-1.5 text-sm font-medium">
                    Used as
                    <select className={inputCls} value={editing.level} onChange={set('level')}>
                      {Object.entries(LEVELS).map(([k, v]) => (
                        <option key={k} value={k}>
                          {v}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="grid gap-1.5 text-sm font-medium">
                    Values
                    <select className={inputCls} value={editing.kind} onChange={set('kind')}>
                      {Object.entries(KINDS).map(([k, v]) => (
                        <option key={k} value={k}>
                          {v}
                        </option>
                      ))}
                    </select>
                  </label>
                </>
              )}
              {editing.kind === 'choice' && (
                <label className="grid gap-1.5 text-sm font-medium">
                  Choices <span className="text-xs font-normal text-muted-foreground">One per line, in the order they should appear.</span>
                  <textarea
                    className="min-h-32 w-full rounded-lg border border-input bg-card px-3 py-2 text-base sm:text-sm"
                    value={editing.choicesText}
                    onChange={set('choicesText')}
                    aria-invalid={!!fields.choices}
                  />
                </label>
              )}
              <div className="grid gap-3 sm:grid-cols-2">
                <label className="grid gap-1.5 text-sm font-medium">
                  Order
                  <input type="number" min="0" max="10000" className={inputCls} value={editing.sort} onChange={set('sort')} />
                </label>
                <label className="grid gap-1.5 text-sm font-medium">
                  Status
                  <select className={inputCls} value={editing.status} onChange={set('status')}>
                    <option value="Active">Active</option>
                    <option value="Inactive">Inactive</option>
                  </select>
                </label>
              </div>
              <DialogFooter className="gap-2">
                <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => setEditing(null)}>
                  Cancel
                </Button>
                <Button type="submit" className="max-sm:h-11" disabled={busy || !editing.label.trim()}>
                  {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />} Save
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
