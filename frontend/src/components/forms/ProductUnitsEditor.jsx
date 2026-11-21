import React from 'react';
import { PlusCircleIcon, XMarkIcon } from '@heroicons/react/24/outline';
import { MAX_UNITS, emptyUnitRow } from '../../lib/productUnits';
import { qtyStep } from '../../lib/qty';

// ProductUnitsEditor edits a product's other units (H3): each row is a
// unit the product is also sold or bought in and how many base units it
// holds ("Box holds 12 Piece"). Stock always stays in the base unit.
// rows/onChange are the editor rows (lib/productUnits); units is the
// shop's unit list; baseUnitId is the product's unit.
export default function ProductUnitsEditor({ rows, onChange, units = [], baseUnitId, fractional = false, problem = '' }) {
  const base = units.find((u) => String(u.id) === String(baseUnitId));
  const baseName = base?.name || 'base units';
  const set = (i, patch) => onChange(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  const choices = units.filter((u) => String(u.id) !== String(baseUnitId));

  return (
    <div className="space-y-2">
      <p className="text-xs text-gray-500">
        {baseUnitId
          ? `Stock stays in ${baseName}. Add a unit the product is also sold or bought in, such as a box or a carton.`
          : "Choose the product's unit first. Other units are counted in it."}
      </p>
      {rows.map((r, i) => (
        <div key={i} className="flex flex-wrap items-center gap-2 rounded-lg border border-gray-200 p-2">
          <select
            aria-label={`Other unit ${i + 1}`}
            className="min-w-[8rem] flex-1 px-3 py-2 border border-gray-600 rounded-lg"
            value={r.unitId}
            disabled={!baseUnitId}
            onChange={(e) => set(i, { unitId: e.target.value })}
          >
            <option value="">Select unit</option>
            {choices.map((u) => (
              <option key={u.id} value={u.id}>{u.name}</option>
            ))}
          </select>
          <span className="text-sm text-gray-600">holds</span>
          <input
            aria-label={`${baseName} in other unit ${i + 1}`}
            type="number"
            min="0"
            step={qtyStep(fractional)}
            className="w-24 px-3 py-2 border border-gray-600 rounded-lg"
            value={r.factor}
            disabled={!baseUnitId}
            onChange={(e) => set(i, { factor: e.target.value })}
          />
          <span className="text-sm text-gray-600">{baseName}</span>
          <label className="flex items-center gap-1 text-sm text-gray-700">
            <input type="checkbox" checked={r.forSale} onChange={(e) => set(i, { forSale: e.target.checked })} />
            Sale
          </label>
          <label className="flex items-center gap-1 text-sm text-gray-700">
            <input type="checkbox" checked={r.forPurchase} onChange={(e) => set(i, { forPurchase: e.target.checked })} />
            Purchase
          </label>
          <button
            type="button"
            aria-label={`Remove other unit ${i + 1}`}
            className="ml-auto rounded p-1 text-gray-500 hover:bg-gray-100 hover:text-red-600"
            onClick={() => onChange(rows.filter((_, j) => j !== i))}
          >
            <XMarkIcon className="h-5 w-5" />
          </button>
        </div>
      ))}
      {problem && <p role="alert" className="text-xs text-red-600">{problem}</p>}
      {rows.length < MAX_UNITS && (
        <button
          type="button"
          className="inline-flex items-center gap-1 text-sm font-medium text-blue-700 hover:text-blue-900 disabled:text-gray-400"
          disabled={!baseUnitId}
          onClick={() => onChange([...rows, emptyUnitRow()])}
        >
          <PlusCircleIcon className="h-5 w-5" />
          Add another unit
        </button>
      )}
    </div>
  );
}
