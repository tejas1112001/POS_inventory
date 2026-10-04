import { useState, useEffect } from 'react';
import { formatCurrency } from '@/lib/format';

export default function Receipt({
  isOpen,
  onClose,
  invoiceNo: passedInvoiceNo,
  customerName = 'Walk-in Customer',
  customerId,
  orderItems = [],
  subtotal = 0,
  discount = 0,
  tax = 0,
  shipping = 0,
  total = 0,
  biller = 'admin',
  storeName = 'ReactronPOS Store',
  storeAddress = 'Main Street Outlet',
  storePhone = '+1 (555) 019-2834',
}) {
  const [displayInvoiceNo, setDisplayInvoiceNo] = useState('');
  const [currentDate, setCurrentDate] = useState('');

  useEffect(() => {
    if (!isOpen) return;
    if (passedInvoiceNo) {
      setDisplayInvoiceNo(passedInvoiceNo);
    } else {
      const datePart = new Date().toISOString().slice(0, 10).replace(/-/g, '');
      const randomPart = Math.floor(1000 + Math.random() * 9000);
      setDisplayInvoiceNo(`INV-${datePart}-${randomPart}`);
    }

    setCurrentDate(
      new Date().toLocaleString('en-US', {
        year: 'numeric',
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      })
    );
  }, [isOpen, passedInvoiceNo]);

  const handlePrint = () => window.print();
  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center print:block">
      {/* Backdrop */}
      <div onClick={onClose} className="absolute inset-0 bg-black/50 print:hidden" />

      {/* Receipt Modal */}
      <div
        id="receipt-modal"
        className="relative mx-4 w-full max-w-md overflow-hidden rounded-xl bg-white shadow-xl print:fixed print:inset-0 print:m-0 print:max-w-none print:rounded-none print:p-4 print:shadow-none"
      >
        {/* Header */}
        <div className="sticky top-0 z-10 flex items-center justify-between border-b bg-white p-4 print:hidden">
          <h2 className="text-xl font-bold text-gray-800">Sales Receipt</h2>
          <button onClick={onClose} className="text-2xl text-gray-500 hover:text-gray-700">
            ×
          </button>
        </div>

        {/* Content */}
        <div
          id="receipt-content"
          className="max-h-[80vh] overflow-y-auto p-4 print:max-h-full print:overflow-visible"
        >
          {/* Store Logo & Info */}
          <div className="mb-6 text-center">
            <div className="mb-1 text-2xl font-bold text-gray-900">{storeName}</div>
            <div className="text-sm text-gray-600">{storeAddress}</div>
            <div className="text-sm text-gray-600">Phone: {storePhone}</div>
          </div>

          <div className="mb-6 border-b border-t border-gray-300 py-2">
            <h3 className="text-center text-lg font-semibold tracking-wide text-gray-800">TAX INVOICE</h3>
          </div>

          {/* Invoice Metadata */}
          <div className="mb-6 grid grid-cols-2 gap-3 text-sm print:text-xs">
            <div>
              <div className="text-gray-500">Invoice #:</div>
              <div className="font-semibold text-gray-900">{displayInvoiceNo}</div>
            </div>
            <div className="text-right">
              <div className="text-gray-500">Date:</div>
              <div className="font-medium text-gray-900">{currentDate}</div>
            </div>
            <div>
              <div className="text-gray-500">Customer:</div>
              <div className="font-medium text-gray-900">{customerName}</div>
            </div>
            <div className="text-right">
              <div className="text-gray-500">Biller:</div>
              <div className="font-medium text-gray-900">{biller}</div>
            </div>
          </div>

          {/* Items Table */}
          <table className="mb-6 w-full text-sm print:text-xs">
            <thead>
              <tr className="border-b border-gray-300 text-gray-700">
                <th className="py-2 text-left font-semibold">Item</th>
                <th className="py-2 text-center font-semibold">Price</th>
                <th className="py-2 text-center font-semibold">Qty</th>
                <th className="py-2 text-right font-semibold">Total</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {orderItems.map((item, i) => {
                const qty = item.quantity ?? item.qty ?? 1;
                const unitPrice = parseFloat(item.price ?? item.unitPrice ?? 0);
                const lineTotal = unitPrice * qty;

                return (
                  <tr key={item.id || i}>
                    <td className="py-2 text-gray-800">
                      {i + 1}. {item.name || item.product?.name || 'Product'}
                    </td>
                    <td className="py-2 text-center text-gray-600">{formatCurrency(unitPrice)}</td>
                    <td className="py-2 text-center text-gray-800 font-medium">{qty}</td>
                    <td className="py-2 text-right font-medium text-gray-900">{formatCurrency(lineTotal)}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>

          {/* Financial Summary */}
          <div className="mb-6 rounded-lg border border-gray-200 p-3 text-sm print:text-xs">
            <div className="flex justify-between py-1 text-gray-600">
              <span>Subtotal:</span>
              <span className="font-medium text-gray-900">{formatCurrency(subtotal)}</span>
            </div>
            {discount > 0 && (
              <div className="flex justify-between py-1 text-red-600">
                <span>Discount:</span>
                <span>-{formatCurrency(discount)}</span>
              </div>
            )}
            <div className="flex justify-between py-1 text-gray-600">
              <span>Tax:</span>
              <span className="font-medium text-gray-900">{formatCurrency(tax)}</span>
            </div>
            {shipping > 0 && (
              <div className="flex justify-between py-1 text-gray-600">
                <span>Shipping:</span>
                <span className="font-medium text-gray-900">{formatCurrency(shipping)}</span>
              </div>
            )}
            <div className="mt-2 flex justify-between border-t border-gray-200 pt-2 text-base font-bold text-gray-900">
              <span>Total:</span>
              <span>{formatCurrency(total)}</span>
            </div>
          </div>

          {/* Footer */}
          <div className="border-t border-gray-200 py-4 text-center print:border-none print:py-2">
            <p className="mb-2 text-xs italic text-gray-500">
              Thank you for shopping with us!
            </p>

            <button
              onClick={handlePrint}
              className="mt-4 rounded-lg bg-black px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 print:hidden"
            >
              Print Receipt
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
