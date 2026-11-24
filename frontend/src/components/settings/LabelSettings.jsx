import { useCallback, useEffect, useState } from 'react';
import { Loader2, Pencil, Plus, Printer, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { SectionCard } from '@/components/shared/section-card';
import { ErrorState } from '@/components/kit/States';
import { api } from '@/services/api';
import { toast } from '../../../shared/toast/store';

const inputCls =
  'h-11 w-full rounded-lg border border-input bg-card px-3 text-base outline-none focus-visible:border-primary focus-visible:ring-[3px] focus-visible:ring-ring/25 sm:h-9 sm:text-sm';

const FIELDS = [
  ['name', 'Product name'],
  ['variant', 'Option (size / colour)'],
  ['price', 'Price'],
  ['mrp', 'MRP (when the product has one)'],
  ['barcode', 'Barcode bars'],
  ['barcodeText', 'Barcode digits'],
  ['sku', 'SKU'],
];

const NUMBERS = [
  ['pageWidth', 'Page width'],
  ['pageHeight', 'Page height'],
  ['labelWidth', 'Label width'],
  ['labelHeight', 'Label height'],
  ['columns', 'Labels across', true],
  ['rows', 'Labels down', true],
  ['marginLeft', 'Left margin'],
  ['marginTop', 'Top margin'],
  ['gapX', 'Gap between columns'],
  ['gapY', 'Gap between rows'],
];

const size = (l) => `${l.labelWidth} × ${l.labelHeight} mm${l.columns * l.rows > 1 ? `, ${l.columns} × ${l.rows} per page` : ''}`;

// LabelSettings (industry P1 item 5, A11): the built-in label layouts and
// the shop's own (page, label size, grid, fields), with a test page to
// check the printer prints at 100 %.
export default function LabelSettings({ canEdit, canPrint }) {
  const [data, setData] = useState(null);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState(null); // { layout, index } (index -1: new)
  const [testing, setTesting] = useState('');

  const load = useCallback(() => {
    setError('');
    api
      .get('/api/settings/labels', { quiet: true })
      .then(setData)
      .catch((err) => setError(err.message));
  }, []);
  useEffect(load, [load]);

  const save = async (layouts) => {
    const r = await api.put('/api/settings/labels', { layouts }, { quiet: true });
    setData(r);
    return r;
  };

  const remove = async (i) => {
    try {
      await save(data.layouts.filter((_, j) => j !== i));
      toast.success('Layout deleted.');
    } catch (err) {
      toast.error(err.message);
    }
  };

  const test = async (id) => {
    setTesting(id);
    try {
      await api.openBlob('/api/labels/pdf', { method: 'POST', body: { layoutId: id, test: true }, quiet: true });
    } catch (err) {
      toast.error(err.message);
    } finally {
      setTesting('');
    }
  };

  if (error) return <ErrorState message={error} onRetry={load} />;
  if (!data) return <div className="h-40 animate-pulse rounded-lg bg-muted" aria-busy="true" aria-label="Loading label layouts" />;

  const row = (l, i) => (
    <li key={l.id} className="flex flex-wrap items-center justify-between gap-2 py-2">
      <div className="min-w-0">
        <div className="text-sm font-medium">{l.name}</div>
        <div className="text-xs text-muted-foreground">
          {size(l)} · page {l.pageWidth} × {l.pageHeight} mm{l.builtin ? ' · built in' : ''}
        </div>
      </div>
      <div className="flex gap-1">
        {canPrint && (
          <Button type="button" variant="outline" size="sm" className="max-sm:h-11" disabled={!!testing} onClick={() => test(l.id)}>
            {testing === l.id ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <Printer className="size-4" aria-hidden="true" />}
            Test page
          </Button>
        )}
        {canEdit && !l.builtin && (
          <>
            <Button type="button" variant="outline" size="sm" className="max-sm:h-11" onClick={() => setEditing({ layout: l, index: i })} aria-label={`Edit ${l.name}`}>
              <Pencil className="size-4" aria-hidden="true" />
            </Button>
            <Button type="button" variant="outline" size="sm" className="text-danger hover:text-danger max-sm:h-11" onClick={() => remove(i)} aria-label={`Delete ${l.name}`}>
              <Trash2 className="size-4" aria-hidden="true" />
            </Button>
          </>
        )}
      </div>
    </li>
  );

  return (
    <SectionCard
      title="Label layouts"
      action={
        canEdit && data.layouts.length < 20 ? (
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="max-sm:h-11"
            onClick={() => setEditing({ layout: { ...data.presets[0], id: '', name: '', builtin: false }, index: -1 })}
          >
            <Plus className="size-4" aria-hidden="true" /> Add layout
          </Button>
        ) : null
      }
    >
      <p className="mb-2 text-sm text-muted-foreground">
        Print a test page on your label printer or A4 label sheets. In the print dialog choose <strong>Actual size</strong> (100 %), not &ldquo;Fit to
        page&rdquo;. If the labels sit off the stickers, add a layout with adjusted margins.
      </p>
      <ul className="divide-y">
        {data.presets.map((l) => row(l, -1))}
        {data.layouts.map((l, i) => row(l, i))}
      </ul>
      {editing && (
        <LayoutDialog
          initial={editing.layout}
          onClose={() => setEditing(null)}
          onSave={async (l) => {
            const next = [...data.layouts];
            if (editing.index < 0) next.push(l);
            else next[editing.index] = l;
            await save(next);
            toast.success('Layout saved.');
            setEditing(null);
          }}
          indexKey={editing.index < 0 ? data.layouts.length : editing.index}
        />
      )}
    </SectionCard>
  );
}

function LayoutDialog({ initial, onClose, onSave, indexKey }) {
  const [l, setL] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const fieldErr = (k) => error?.fields?.[`layouts[${indexKey}].${k}`];
  const set = (k, v) => setL((cur) => ({ ...cur, [k]: v }));
  const toggleField = (f) => set('fields', l.fields.includes(f) ? l.fields.filter((x) => x !== f) : FIELDS.map(([k]) => k).filter((k) => k === f || l.fields.includes(k)));

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    const id = l.id || l.name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 40);
    try {
      await onSave({ ...l, id });
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent sheet className="sm:max-w-2xl">
        <form onSubmit={submit} className="grid gap-4">
          <DialogHeader>
            <DialogTitle>{initial.id ? `Edit ${initial.name}` : 'New label layout'}</DialogTitle>
            <DialogDescription>Sizes are in millimetres. Measure the sticker sheet or roll with a ruler.</DialogDescription>
          </DialogHeader>
          <div className="grid max-h-[60vh] gap-3 overflow-y-auto sm:grid-cols-2">
            <label className="grid gap-1.5 text-sm font-medium sm:col-span-2">
              Name
              <input className={inputCls} value={l.name} maxLength={60} required onChange={(e) => set('name', e.target.value)} />
              {(fieldErr('name') || fieldErr('id')) && <span className="text-xs text-danger">{fieldErr('name') || fieldErr('id')}</span>}
            </label>
            {NUMBERS.map(([k, label, whole]) => (
              <label key={k} className="grid gap-1.5 text-sm font-medium">
                {label}
                {!whole && ' (mm)'}
                <input
                  type="number"
                  min="0"
                  step={whole ? 1 : 0.5}
                  className={inputCls}
                  value={l[k]}
                  required
                  onChange={(e) => set(k, e.target.value === '' ? '' : Number(e.target.value))}
                />
                {fieldErr(k) && <span className="text-xs text-danger">{fieldErr(k)}</span>}
              </label>
            ))}
            <fieldset className="sm:col-span-2">
              <legend className="mb-1 text-sm font-medium">On each label</legend>
              <div className="flex flex-wrap gap-2">
                {FIELDS.map(([k, label]) => (
                  <label key={k} className="flex items-center gap-1.5 rounded-lg border px-2 py-1 text-sm">
                    <input type="checkbox" className="size-4" checked={l.fields.includes(k)} onChange={() => toggleField(k)} />
                    {label}
                  </label>
                ))}
              </div>
              {fieldErr('fields') && <span className="text-xs text-danger">{fieldErr('fields')}</span>}
            </fieldset>
          </div>
          {error && !error.fields && <InlineAlert>{error.message}</InlineAlert>}
          <DialogFooter className="gap-2">
            <Button type="button" variant="outline" className="max-sm:h-11" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" className="max-sm:h-11" disabled={busy}>
              {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />} Save layout
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
