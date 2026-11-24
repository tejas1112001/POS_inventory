import { Link } from "react-router-dom";
import StatusBadge from "@/components/kit/StatusBadge";
import { ProductImage } from "@/components/shared/product-image";
import { formatCurrency, formatDateTime } from "@/lib/format";
import { cn } from "@/lib/utils";

// Dashboard lists (Teal & Cream): recent transactions and low stock.

const PAY_LABEL = {
  cash: "Cash",
  card: "Card",
  upi: "UPI",
  credit: "Credit",
  bank: "Bank",
  cheque: "Cheque",
  split: "Split",
};
function payLabel(m) {
  if (!m) return "—";
  return (
    PAY_LABEL[String(m).toLowerCase()] ||
    String(m).charAt(0).toUpperCase() + String(m).slice(1)
  );
}

// RecentSalesTable: the last five orders (GET /api/sales, first page).
export function RecentSalesTable({ rows }) {
  if (!rows.length)
    return (
      <p className="flex flex-1 items-center justify-center py-8 text-sm text-muted-foreground">
        No orders yet.
      </p>
    );
  return (
    <>
      {/* Phones: one compact row per order */}
      <ul
        className="-my-1 divide-y divide-border/70 sm:hidden"
        aria-label="Recent transactions"
      >
        {rows.map((r) => (
          <li key={r.id} className="flex items-center gap-3 py-2.5">
            <div className="min-w-0 flex-1">
              <Link
                to={`/sales/${r.id}`}
                className="font-semibold text-primary hover:underline"
              >
                {r.reference}
              </Link>
              <p className="truncate text-xs text-muted-foreground">
                {r.customer?.name || "Walk-in customer"} ·{" "}
                {payLabel(r.payment_method)}
              </p>
            </div>
            <div className="shrink-0 text-right">
              <p className="text-sm font-semibold tabular-nums">
                {formatCurrency(r.total)}
              </p>
              <StatusBadge value={r.status} />
            </div>
          </li>
        ))}
      </ul>
      <div className="-mx-4 hidden overflow-x-auto sm:block">
        <table
          className="w-full min-w-[520px] text-sm"
          aria-label="Recent transactions"
        >
          <thead>
            <tr className="bg-muted text-left text-xs font-semibold text-muted-foreground">
              <th scope="col" className="px-4 py-2.5">
                Order
              </th>
              <th scope="col" className="px-3 py-2.5">
                Customer
              </th>
              <th scope="col" className="px-3 py-2.5 text-right">
                Amount
              </th>
              <th scope="col" className="px-3 py-2.5">
                Payment
              </th>
              <th scope="col" className="px-4 py-2.5">
                Status
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border/70">
            {rows.map((r) => (
              <tr key={r.id} className="transition-colors hover:bg-accent/40">
                <td className="px-4 py-2.5">
                  <Link
                    to={`/sales/${r.id}`}
                    className="font-semibold text-primary hover:underline"
                  >
                    {r.reference}
                  </Link>
                  <span className="block text-xs text-muted-foreground">
                    {formatDateTime(r.date)}
                  </span>
                </td>
                <td className="max-w-[160px] truncate px-3 py-2.5">
                  {r.customer?.name || "Walk-in customer"}
                </td>
                <td className="px-3 py-2.5 text-right font-semibold tabular-nums">
                  {formatCurrency(r.total)}
                </td>
                <td className="px-3 py-2.5">
                  <span className="inline-flex rounded-md bg-teal-soft px-2 py-1 text-xs leading-none font-semibold text-accent-foreground">
                    {payLabel(r.payment_method)}
                  </span>
                </td>
                <td className="px-4 py-2.5">
                  <StatusBadge value={r.status} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}

// LowStockList: items at or under their alert level (GET /api/inventory).
export function LowStockList({ rows }) {
  if (!rows.length)
    return (
      <p className="flex flex-1 items-center justify-center py-8 text-sm text-muted-foreground">
        Nothing is running low.
      </p>
    );
  return (
    <ul
      className="-my-1 divide-y divide-border/70"
      aria-label="Low stock items"
    >
      {rows.map((r) => (
        <li
          key={`${r.storeId}-${r.variantId ?? r.id}`}
          className="flex items-center gap-3 py-2.5"
        >
          <ProductImage name={r.productName} src={r.productImage} size={40} />
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-semibold text-foreground">
              {r.productName}
            </p>
            <p className="truncate text-xs text-muted-foreground">
              {[r.sku && `SKU: ${r.sku}`, r.storeName]
                .filter(Boolean)
                .join(" · ")}
            </p>
          </div>
          <span
            className={cn(
              "shrink-0 rounded-md px-2 py-1 text-xs leading-none font-semibold tabular-nums",
              Number(r.qty) <= 0
                ? "bg-danger-bg text-danger-fg"
                : "bg-warning-bg text-warning-fg",
            )}
          >
            Stock: {r.qty}
          </span>
        </li>
      ))}
    </ul>
  );
}
