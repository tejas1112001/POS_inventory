import { toast } from '../../shared/toast/store';
import { useEffect, useMemo, useRef, useState } from 'react';
import { Download, Loader2, Mail, Printer } from 'lucide-react';
import { api } from '@/services/api';
import { usePermission } from '@/context/PermissionContext';
import { usePos } from '@/hooks/usePos';
import { useShopSettings } from '@/hooks/useShopSettings';
import { formatCurrency, formatDateTime } from '@/lib/format';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { useImageSrc } from '@/components/shared/auth-image';
import { cn } from '@/lib/utils';
import './receipt.css';
import receiptCss from './receipt.css?raw';

const n = (v) => Number(v) || 0;
const pct = (v) => `${Number(n(v).toFixed(3))}%`;
const lines = (s) => (s || '').split('\n').map((x) => x.trim()).filter(Boolean);

// Paper sizes for printing; the choice is kept in this browser.
const PAPERS = [
  { value: 'a4', label: 'A4', page: '@page { size: A4; margin: 12mm; }', narrow: false },
  { value: '80mm', label: '80 mm', page: '@page { size: 80mm auto; margin: 3mm; } body { width: 74mm; }', narrow: true },
  { value: '58mm', label: '58 mm', page: '@page { size: 58mm auto; margin: 2mm; } body { width: 54mm; }', narrow: true },
];
const PAPER_KEY = 'pos.receiptPaper';
function savedPaper() {
  try {
    const v = localStorage.getItem(PAPER_KEY);
    return PAPERS.some((p) => p.value === v) ? v : 'a4';
  } catch {
    return 'a4';
  }
}

function attrText(a) {
  if (!a || typeof a !== 'object') return '';
  return Object.keys(a)
    .sort()
    .filter((k) => a[k] !== '' && a[k] != null)
    .map((k) => `${k}: ${a[k]}`)
    .join(', ');
}

// taxRows: the PDF's tax summary, from the stored lines ("CGST 9% on 1,000.00").
function taxRows(items, taxLabel) {
  const by = new Map();
  for (const i of items) {
    if (n(i.tax_rate) <= 0) continue;
    const k = String(n(i.tax_rate));
    const a = by.get(k) || { rate: n(i.tax_rate), taxable: 0, tax: 0, cgst: 0, sgst: 0, igst: 0 };
    a.taxable += n(i.subtotal) - n(i.tax_amount);
    a.tax += n(i.tax_amount);
    a.cgst += n(i.cgst);
    a.sgst += n(i.sgst);
    a.igst += n(i.igst);
    by.set(k, a);
  }
  const out = [];
  for (const a of by.values()) {
    if (a.cgst > 0 || a.sgst > 0) {
      out.push({ label: `CGST ${pct(a.rate / 2)}`, taxable: a.taxable, amount: a.cgst });
      out.push({ label: `SGST ${pct(a.rate / 2)}`, taxable: a.taxable, amount: a.sgst });
    } else if (a.igst > 0) out.push({ label: `IGST ${pct(a.rate)}`, taxable: a.taxable, amount: a.igst });
    else out.push({ label: `${taxLabel} ${pct(a.rate)}`, taxable: a.taxable, amount: a.tax });
  }
  return out;
}

// printNode prints the receipt alone, in a hidden frame, on the chosen paper.
async function printNode(node, paper, title) {
  const frame = document.createElement('iframe');
  frame.setAttribute('aria-hidden', 'true');
  frame.tabIndex = -1;
  Object.assign(frame.style, { position: 'fixed', right: '0', bottom: '0', width: '0', height: '0', border: '0' });
  document.body.appendChild(frame);
  const doc = frame.contentDocument;
  const clone = node.cloneNode(true);
  clone.classList.toggle('rc-narrow', paper.narrow);
  doc.open();
  doc.write(
    `<!doctype html><html><head><meta charset="utf-8"><style>${receiptCss}\nhtml,body{margin:0;background:#fff;-webkit-print-color-adjust:exact;print-color-adjust:exact}\n${paper.page}</style></head><body></body></html>`
  );
  doc.close();
  doc.title = title;
  doc.body.appendChild(doc.importNode(clone, true));
  await Promise.all(
    [...doc.images].map((img) => (img.complete ? null : new Promise((r) => (img.onload = img.onerror = r))))
  );
  const remove = () => setTimeout(() => frame.remove(), 500);
  frame.contentWindow.addEventListener('afterprint', remove, { once: true });
  setTimeout(() => frame.isConnected && frame.remove(), 60_000);
  frame.contentWindow.focus();
  frame.contentWindow.print();
}

function ReceiptBody({ sale, store, settings, businessName, taxLabel, logoUrl, narrow, bodyRef }) {
  const items = sale.items || [];
  const rows = taxRows(items, taxLabel);
  const taxReg = settings?.taxRegistrationNo;
  const taxId = taxReg ? (taxLabel === 'GST' ? `GSTIN: ${taxReg}` : `${taxLabel} No: ${taxReg}`) : '';
  const address = lines(settings?.address);
  const contact = [settings?.phone, settings?.email].filter(Boolean).join(' · ');
  const storeLine = [store?.name || sale.Store?.name, store?.address, store?.phone].filter(Boolean).join(', ');
  const voided = sale.status === 'Void';
  const tendered = sale.tendered != null && n(sale.tendered) > 0;

  return (
    <div ref={bodyRef} className={cn('rc', narrow && 'rc-narrow')}>
      {voided && (
        <div className="rc-void">
          <strong>VOID</strong>
          This sale was voided{sale.voided_at ? ` on ${formatDateTime(sale.voided_at)}` : ''}
          {sale.void_reason ? `: ${sale.void_reason}` : ''}.
        </div>
      )}
      <div className="rc-head">
        {logoUrl && <img className="rc-logo" src={logoUrl} alt="" />}
        <div>
          <p className="rc-biz">{businessName}</p>
          {settings?.legalName && settings.legalName !== businessName && <p className="rc-muted">{settings.legalName}</p>}
          {address.map((l, i) => (
            <p key={i} className="rc-muted">
              {l}
            </p>
          ))}
          {contact && <p className="rc-muted">{contact}</p>}
          {taxId && <p className="rc-muted">{taxId}</p>}
        </div>
      </div>
      <hr className="rc-rule" />
      <div className="rc-meta">
        <p className="rc-title">{taxReg ? 'Tax Invoice' : 'Receipt'}</p>
        <p className="rc-title rc-r">{sale.reference}</p>
        <p>Store: {storeLine}</p>
        <p className="rc-r">{formatDateTime(sale.date)}</p>
        <p>
          Customer: {sale.customer?.name || 'Walk-in Customer'}
          {sale.receipt_email ? ` (${sale.receipt_email})` : ''}
        </p>
        <p className="rc-r">Cashier: {sale.user?.name || ''}</p>
      </div>
      {lines(settings?.receiptHeader).map((l, i) => (
        <p key={i} className="rc-center rc-muted">
          {l}
        </p>
      ))}
      <table className="rc-items">
        <thead>
          <tr>
            <th>Item</th>
            <th>Qty</th>
            <th>Price</th>
            <th>Disc.</th>
            <th>Tax</th>
            <th>Total</th>
          </tr>
        </thead>
        <tbody>
          {items.map((i, k) => {
            const attrs = attrText(i.variant?.attributes);
            return (
              <tr key={i.id || k}>
                <td>
                  {i.product_name || i.name}
                  {attrs && ` (${attrs})`}
                  {i.sku && <span className="rc-sku">SKU {i.sku}</span>}
                </td>
                <td className="rc-qty">{i.quantity}</td>
                <td>{formatCurrency(i.unit_price)}</td>
                {n(i.discount) > 0 ? <td data-label="Disc.">{formatCurrency(i.discount)}</td> : <td className="rc-empty" />}
                <td data-label="Tax">{pct(i.tax_rate)}</td>
                <td>{formatCurrency(i.subtotal)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <div className="rc-totals">
        <div>
          <span>Subtotal</span>
          <span>{formatCurrency(sale.subtotal)}</span>
        </div>
        {n(sale.discount) > 0 && (
          <div>
            <span>Discount</span>
            <span>-{formatCurrency(sale.discount)}</span>
          </div>
        )}
        {rows.map((r) => (
          <div key={r.label} className="rc-muted">
            <span>
              {r.label} on {formatCurrency(r.taxable)}
            </span>
            <span>{formatCurrency(r.amount)}</span>
          </div>
        ))}
        <div>
          <span>Tax</span>
          <span>{formatCurrency(sale.tax)}</span>
        </div>
        {n(sale.shipping) > 0 && (
          <div>
            <span>Shipping</span>
            <span>{formatCurrency(sale.shipping)}</span>
          </div>
        )}
        <div className="rc-grand">
          <span>Total</span>
          <span>{formatCurrency(sale.total)}</span>
        </div>
        <div>
          <span>Paid</span>
          <span>{formatCurrency(sale.paid)}</span>
        </div>
        {tendered && (
          <>
            <div>
              <span>Received</span>
              <span>{formatCurrency(sale.tendered)}</span>
            </div>
            <div>
              <span>Change</span>
              <span>{formatCurrency(sale.change)}</span>
            </div>
          </>
        )}
        {n(sale.due) > 0 && (
          <div className="rc-grand">
            <span>Due</span>
            <span>{formatCurrency(sale.due)}</span>
          </div>
        )}
        {sale.payment_method && (
          <div>
            <span>Payment</span>
            <span>
              {sale.payment_method} ({sale.payment_status})
            </span>
          </div>
        )}
      </div>
      <div className="rc-lines">
        {lines(settings?.receiptFooter).map((l, i) => (
          <p key={i} className="rc-center rc-muted">
            {l}
          </p>
        ))}
      </div>
      <p className="rc-thanks">Thank you for your purchase.</p>
    </div>
  );
}

// Receipt shows a sale as the server stored it (totals, tax and lines are
// the server's) in the same layout as the PDF. It prints on A4 or thermal
// paper, downloads the PDF and emails it again. Pass the sale, or its ID.
export default function Receipt({ isOpen = true, onClose, sale: given, saleId, store }) {
  const { hasPermission } = usePermission();
  const { tenant } = usePos();
  const { settings } = useShopSettings();
  const [sale, setSale] = useState(given || null);
  const [loadError, setLoadError] = useState('');
  const [email, setEmail] = useState(given?.receipt_email || '');
  const [busy, setBusy] = useState('');
  const [paper, setPaper] = useState(savedPaper);
  const [phone, setPhone] = useState(() => typeof window !== 'undefined' && window.matchMedia?.('(max-width: 479px)').matches);
  const bodyRef = useRef(null);
  const logoUrl = useImageSrc(settings?.logo || '');

  useEffect(() => {
    const mq = window.matchMedia?.('(max-width: 479px)');
    if (!mq) return;
    const on = () => setPhone(mq.matches);
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, []);

  useEffect(() => {
    if (given) {
      setSale(given);
      setEmail(given.receipt_email || '');
      return;
    }
    if (!saleId) return;
    let active = true;
    api
      .get(`/api/sales/${saleId}`, { quiet: true })
      .then((s) => {
        if (!active) return;
        setSale(s);
        setEmail(s.receipt_email || '');
      })
      .catch((err) => active && setLoadError(err.message));
    return () => {
      active = false;
    };
  }, [given, saleId]);

  const paperDef = useMemo(() => PAPERS.find((p) => p.value === paper) || PAPERS[0], [paper]);

  const canDownload = hasPermission('receipts', 'view');
  const canResend = hasPermission('receipts', 'resend');
  const taxLabel = settings?.taxLabel || 'Tax';
  const businessName = settings?.businessName || tenant?.name || '';
  const voided = sale?.status === 'Void';

  const choosePaper = (v) => {
    setPaper(v);
    try {
      localStorage.setItem(PAPER_KEY, v);
    } catch {
      /* private window: not kept */
    }
  };

  const print = async () => {
    if (!bodyRef.current) return;
    try {
      await printNode(bodyRef.current, paperDef, sale?.reference || 'Receipt');
    } catch {
      toast.error('Printing did not start. Use Download PDF and print the file.');
    }
  };

  const download = async () => {
    setBusy('pdf');
    try {
      await api.download(`/api/sales/${sale.id}/receipt`, `${sale.reference || 'receipt'}.pdf`, { quiet: true });
    } catch (err) {
      toast.error(err.code === 'UNAVAILABLE' ? 'The PDF is still being prepared. Try again in a moment.' : err.message);
    } finally {
      setBusy('');
    }
  };

  const resend = async (e) => {
    e.preventDefault();
    setBusy('mail');
    try {
      const body = email.trim() ? { email: email.trim() } : {};
      await api.post(`/api/sales/${sale.id}/receipt/resend`, body, { quiet: true });
      toast.success(email.trim() ? `The receipt will be emailed to ${email.trim()}.` : "The receipt will be emailed to the customer's address.");
    } catch (err) {
      toast.error(err.code === 'RATE_LIMITED' ? 'This address already got 5 receipts today. Try again tomorrow.' : err.fields?.email || err.message);
    } finally {
      setBusy('');
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={(o) => !o && onClose()}>
      <DialogContent sheet className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{voided ? 'Receipt (voided)' : 'Receipt'}</DialogTitle>
          <DialogDescription className="sr-only">The sale receipt, with print, PDF and email.</DialogDescription>
        </DialogHeader>

        {!sale ? (
          <div className="py-10 text-center text-sm text-muted-foreground">
            {loadError || (
              <span className="inline-flex items-center gap-2">
                <Loader2 className="size-4 animate-spin" aria-hidden="true" /> Loading receipt...
              </span>
            )}
          </div>
        ) : (
          <div className="rounded-lg bg-muted p-2 sm:p-4">
            <div
              className={cn(
                'mx-auto rounded-sm bg-white p-4 shadow-sm ring-1 ring-border',
                paperDef.value === '80mm' && 'max-w-[302px]',
                paperDef.value === '58mm' && 'max-w-[220px] p-2'
              )}
            >
              <ReceiptBody
                sale={sale}
                store={store}
                settings={settings}
                businessName={businessName}
                taxLabel={taxLabel}
                logoUrl={logoUrl}
                narrow={paperDef.narrow || phone}
                bodyRef={bodyRef}
              />
            </div>
          </div>
        )}

        {sale && (
          <DialogFooter className="flex-col gap-2 sm:flex-col sm:justify-start">
            {canResend && !voided && (
              <form onSubmit={resend} className="flex w-full gap-2">
                <Input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="Customer email (optional)"
                  aria-label="Email address for the receipt"
                  className="h-10 min-w-0 flex-1"
                />
                <Button type="submit" variant="outline" className="h-10" disabled={busy === 'mail'}>
                  {busy === 'mail' ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <Mail className="size-4" aria-hidden="true" />} Email
                </Button>
              </form>
            )}
            <div className="flex w-full flex-wrap items-center gap-2">
              <label className="flex items-center gap-2 text-sm text-muted-foreground">
                Paper
                <select className="h-10 rounded-md border bg-card px-2 text-foreground" value={paper} onChange={(e) => choosePaper(e.target.value)}>
                  {PAPERS.map((p) => (
                    <option key={p.value} value={p.value}>
                      {p.label}
                    </option>
                  ))}
                </select>
              </label>
              <div className="ml-auto flex gap-2">
                {canDownload && (
                  <Button type="button" variant="outline" className="h-10" onClick={download} disabled={busy === 'pdf'}>
                    {busy === 'pdf' ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <Download className="size-4" aria-hidden="true" />}
                    <span className="hidden sm:inline">Download</span> PDF
                  </Button>
                )}
                <Button type="button" className="h-10" onClick={print}>
                  <Printer className="size-4" aria-hidden="true" /> Print
                </Button>
              </div>
            </div>
          </DialogFooter>
        )}
      </DialogContent>
    </Dialog>
  );
}
