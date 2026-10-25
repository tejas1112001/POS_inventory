import { useEffect, useMemo, useState } from 'react';
import { Download, Loader2, Mail, Printer, X } from 'lucide-react';
import { api } from '@/services/api';
import { usePermission } from '@/context/PermissionContext';
import { usePos } from '@/hooks/usePos';
import { useShopSettings } from '@/hooks/useShopSettings';
import { formatCurrency, formatDateTime } from '@/lib/format';

const n = (v) => Number(v) || 0;

// Receipt shows a sale as the server stored it (totals, tax and lines are
// the server's), with the shop's details from settings. It can print,
// download the PDF receipt and email it again. Pass the sale, or its ID.
export default function Receipt({ isOpen = true, onClose, sale: given, saleId, store }) {
  const { hasPermission } = usePermission();
  const { tenant } = usePos();
  const { settings } = useShopSettings();
  const [sale, setSale] = useState(given || null);
  const [loadError, setLoadError] = useState('');
  const [email, setEmail] = useState(given?.receipt_email || '');
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState(null);

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

  const taxParts = useMemo(() => {
    const items = sale?.items || [];
    const sum = (k) => items.reduce((acc, i) => acc + n(i[k]), 0);
    return { cgst: sum('cgst'), sgst: sum('sgst'), igst: sum('igst') };
  }, [sale]);

  if (!isOpen) return null;

  const canDownload = hasPermission('receipts', 'view');
  const canResend = hasPermission('receipts', 'resend');
  const taxLabel = settings?.taxLabel || 'Tax';
  const businessName = settings?.businessName || tenant?.name || '';
  const voided = sale?.status === 'Void';

  const download = async () => {
    setBusy('pdf');
    setNotice(null);
    try {
      await api.download(`/api/sales/${sale.id}/receipt`, `${sale.reference || 'receipt'}.pdf`, { quiet: true });
    } catch (err) {
      setNotice({ kind: 'error', text: err.code === 'UNAVAILABLE' ? 'The PDF is still being prepared. Try again in a moment.' : err.message });
    } finally {
      setBusy('');
    }
  };

  const resend = async (e) => {
    e.preventDefault();
    setBusy('mail');
    setNotice(null);
    try {
      const body = email.trim() ? { email: email.trim() } : {};
      await api.post(`/api/sales/${sale.id}/receipt/resend`, body, { quiet: true });
      setNotice({ kind: 'ok', text: email.trim() ? `The receipt will be emailed to ${email.trim()}.` : "The receipt will be emailed to the customer's address." });
    } catch (err) {
      const text =
        err.code === 'RATE_LIMITED'
          ? 'This address already got 5 receipts today. Try again tomorrow.'
          : err.fields?.email || err.message;
      setNotice({ kind: 'error', text });
    } finally {
      setBusy('');
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center print:block">
      <div onClick={onClose} className="absolute inset-0 bg-black/50 print:hidden" />
      <div
        id="receipt-modal"
        role="dialog"
        aria-modal="true"
        aria-label="Receipt"
        className="relative mx-4 w-full max-w-md overflow-hidden rounded-xl bg-white text-gray-900 shadow-xl print:fixed print:inset-0 print:m-0 print:max-w-none print:rounded-none print:p-4 print:shadow-none"
      >
        <div className="flex items-center justify-between border-b p-4 print:hidden">
          <h2 className="text-lg font-bold">Receipt</h2>
          <button onClick={onClose} className="rounded p-1 text-gray-500 hover:bg-gray-100" aria-label="Close">
            <X className="size-5" />
          </button>
        </div>

        <div className="max-h-[75vh] overflow-y-auto p-4 print:max-h-full print:overflow-visible">
          {!sale ? (
            <div className="py-10 text-center text-sm text-gray-500">
              {loadError || (
                <span className="inline-flex items-center gap-2">
                  <Loader2 className="size-4 animate-spin" /> Loading receipt...
                </span>
              )}
            </div>
          ) : (
            <>
              <div className="mb-4 text-center">
                {settings?.receiptHeader && <div className="mb-1 text-xs text-gray-600">{settings.receiptHeader}</div>}
                <div className="text-xl font-bold">{businessName}</div>
                {settings?.legalName && settings.legalName !== businessName && (
                  <div className="text-xs text-gray-600">{settings.legalName}</div>
                )}
                {(store?.name || sale.Store?.name) && <div className="text-sm text-gray-700">{store?.name || sale.Store?.name}</div>}
                {(store?.address || settings?.address) && <div className="text-xs text-gray-600">{store?.address || settings.address}</div>}
                {(store?.phone || settings?.phone) && <div className="text-xs text-gray-600">Phone: {store?.phone || settings.phone}</div>}
                {settings?.taxRegistrationNo && (
                  <div className="text-xs text-gray-600">
                    {taxLabel} No.: {settings.taxRegistrationNo}
                  </div>
                )}
              </div>

              <div className="mb-4 border-y border-gray-300 py-1.5 text-center text-sm font-semibold tracking-wide">
                {voided ? 'VOIDED SALE' : settings?.taxRegistrationNo ? 'TAX INVOICE' : 'RECEIPT'}
              </div>

              <div className="mb-4 grid grid-cols-2 gap-2 text-xs">
                <div>
                  <div className="text-gray-500">Receipt no.</div>
                  <div className="font-semibold">{sale.reference}</div>
                </div>
                <div className="text-right">
                  <div className="text-gray-500">Date</div>
                  <div className="font-medium">{formatDateTime(sale.date)}</div>
                </div>
                <div>
                  <div className="text-gray-500">Customer</div>
                  <div className="font-medium">{sale.customer?.name || 'Walk-in Customer'}</div>
                </div>
                <div className="text-right">
                  <div className="text-gray-500">Cashier</div>
                  <div className="font-medium">{sale.user?.name || ''}</div>
                </div>
              </div>

              <table className="mb-4 w-full text-xs">
                <thead>
                  <tr className="border-b border-gray-300 text-gray-600">
                    <th className="py-1.5 text-left font-semibold">Item</th>
                    <th className="py-1.5 text-right font-semibold">Price</th>
                    <th className="py-1.5 text-center font-semibold">Qty</th>
                    <th className="py-1.5 text-right font-semibold">Amount</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {(sale.items || []).map((item, i) => (
                    <tr key={item.id || i}>
                      <td className="py-1.5">
                        {item.product_name || item.name}
                        {item.sku && <div className="text-[10px] text-gray-500">{item.sku}</div>}
                      </td>
                      <td className="py-1.5 text-right">{formatCurrency(item.unit_price)}</td>
                      <td className="py-1.5 text-center">{item.quantity}</td>
                      <td className="py-1.5 text-right font-medium">
                        {/* Before tax, so the lines add up to the subtotal. */}
                        {formatCurrency(n(item.unit_price) * n(item.quantity) - n(item.discount))}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>

              <div className="mb-4 space-y-1 rounded-lg border border-gray-200 p-3 text-xs">
                <Row label="Subtotal" value={sale.subtotal} />
                {n(sale.discount) > 0 && <Row label="Discount" value={-n(sale.discount)} className="text-red-600" />}
                {taxParts.cgst > 0 || taxParts.sgst > 0 ? (
                  <>
                    <Row label="CGST" value={taxParts.cgst} />
                    <Row label="SGST" value={taxParts.sgst} />
                  </>
                ) : null}
                {taxParts.igst > 0 && <Row label="IGST" value={taxParts.igst} />}
                {!(taxParts.cgst > 0 || taxParts.sgst > 0 || taxParts.igst > 0) && <Row label={taxLabel} value={sale.tax} />}
                {n(sale.shipping) > 0 && <Row label="Shipping" value={sale.shipping} />}
                <div className="mt-1 flex justify-between border-t border-gray-200 pt-1.5 text-sm font-bold">
                  <span>Total</span>
                  <span>{formatCurrency(sale.total)}</span>
                </div>
                <Row label={`Paid${sale.payment_method ? ` (${sale.payment_method})` : ''}`} value={sale.paid} />
                {sale.tendered != null && n(sale.tendered) > 0 && <Row label="Received" value={sale.tendered} />}
                {sale.change != null && n(sale.change) > 0 && <Row label="Change" value={sale.change} />}
                {n(sale.due) > 0 && <Row label="Due" value={sale.due} className="font-semibold text-amber-700" />}
              </div>

              <p className="text-center text-xs italic text-gray-500">{settings?.receiptFooter || 'Thank you for shopping with us!'}</p>

              {notice && (
                <div
                  role={notice.kind === 'error' ? 'alert' : 'status'}
                  className={`mt-4 rounded-md px-3 py-2 text-xs print:hidden ${notice.kind === 'error' ? 'bg-red-50 text-red-700' : 'bg-emerald-50 text-emerald-800'}`}
                >
                  {notice.text}
                </div>
              )}

              <div className="mt-4 flex flex-wrap justify-center gap-2 print:hidden">
                <button
                  onClick={() => window.print()}
                  className="inline-flex items-center gap-1.5 rounded-lg bg-black px-3 py-2 text-sm font-medium text-white hover:bg-gray-800"
                >
                  <Printer className="size-4" /> Print
                </button>
                {canDownload && (
                  <button
                    onClick={download}
                    disabled={busy === 'pdf'}
                    className="inline-flex items-center gap-1.5 rounded-lg border px-3 py-2 text-sm font-medium hover:bg-gray-50 disabled:opacity-60"
                  >
                    {busy === 'pdf' ? <Loader2 className="size-4 animate-spin" /> : <Download className="size-4" />} Download PDF
                  </button>
                )}
              </div>

              {canResend && !voided && (
                <form onSubmit={resend} className="mt-4 flex gap-2 border-t pt-4 print:hidden">
                  <input
                    type="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder="Customer email (optional)"
                    aria-label="Email address for the receipt"
                    className="h-9 min-w-0 flex-1 rounded-md border px-2 text-sm"
                  />
                  <button
                    type="submit"
                    disabled={busy === 'mail'}
                    className="inline-flex items-center gap-1.5 rounded-lg border px-3 text-sm font-medium hover:bg-gray-50 disabled:opacity-60"
                  >
                    {busy === 'mail' ? <Loader2 className="size-4 animate-spin" /> : <Mail className="size-4" />} Email
                  </button>
                </form>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function Row({ label, value, className = '' }) {
  return (
    <div className={`flex justify-between ${className}`}>
      <span className="text-gray-600">{label}</span>
      <span className="font-medium">{formatCurrency(value)}</span>
    </div>
  );
}
