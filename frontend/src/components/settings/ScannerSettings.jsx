import { useState } from 'react';
import { SectionCard } from '@/components/shared/section-card';
import { DEFAULT_GAP_MS, scanGap, setScanGap } from '@/lib/scan';
import { useScanner } from '@/hooks/useScanner';

// ScannerSettings (industry P1 item 3): test a barcode scanner on this
// browser and, for a slow (often Bluetooth) scanner, raise the time
// allowed between its keystrokes. Stored in this browser only.
export default function ScannerSettings() {
  const [gap, setGap] = useState(scanGap());
  const [last, setLast] = useState(null);
  const [typed, setTyped] = useState('');
  useScanner((code) => setLast({ code, at: new Date() }), { accept: (t) => t.id === 'scanner-test' });

  const save = (v) => {
    const n = Math.min(200, Math.max(10, Number(v) || DEFAULT_GAP_MS));
    setGap(n);
    setScanGap(n);
  };

  return (
    <SectionCard title="Barcode scanner">
      <p className="mb-3 text-sm text-muted-foreground">
        A USB or Bluetooth scanner set up as a keyboard works at checkout, returns, purchases, stock counts and label printing.
        Click the box and scan a barcode to test it. These settings apply to this browser only.
      </p>
      <div className="grid gap-4 md:grid-cols-2">
        <label className="space-y-1.5 text-sm font-medium" htmlFor="scanner-test">
          Test scan
          <input
            id="scanner-test"
            className="block h-10 w-full rounded-md border bg-card px-3 font-normal"
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            placeholder="Click here, then scan"
            autoComplete="off"
          />
        </label>
        <label className="space-y-1.5 text-sm font-medium" htmlFor="scanner-gap">
          Time between scanner keystrokes (ms)
          <input
            id="scanner-gap"
            type="number"
            min="10"
            max="200"
            step="5"
            className="block h-10 w-full rounded-md border bg-card px-3 font-normal"
            value={gap}
            onChange={(e) => save(e.target.value)}
          />
          <span className="block text-xs font-normal text-muted-foreground">
            Default {DEFAULT_GAP_MS}. Raise it only if a scan is not recognised (shown below) but the code appears in the box.
          </span>
        </label>
      </div>
      <p className="mt-3 text-sm" role="status" aria-live="polite">
        {last ? (
          <>
            Scan recognised: <span className="font-mono font-semibold">{last.code}</span> ({last.code.length} characters)
          </>
        ) : (
          <span className="text-muted-foreground">No scan yet.</span>
        )}
      </p>
    </SectionCard>
  );
}
