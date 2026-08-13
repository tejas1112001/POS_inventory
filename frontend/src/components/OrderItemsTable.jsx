import React from "react";

const OrderItemsTable = ({ items }) => {
  const totalQty = items.reduce((sum, item) => sum + item.quantity, 0);
  const totalValue = items.reduce((sum, item) => sum + item.subtotal, 0);

  return (
    <div className="pos-table-wrap mt-3">
      <table className="pos-table">
        <thead>
          <tr>
            <th>Product (Code - Name)</th>
            <th>Net Unit Cost</th>
            <th>Quantity</th>
            <th>Discount</th>
            <th>Subtotal</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item, idx) => (
            <tr key={idx}>
              <td>{item.code} - {item.name}</td>
              <td>Rs. {item.cost}</td>
              <td>{item.quantity}</td>
              <td>{item.discount}%</td>
              <td>Rs. {item.subtotal}</td>
            </tr>
          ))}
        </tbody>
        <tfoot>
          <tr className="bg-muted/40 font-semibold">
            <td colSpan="2">Total</td>
            <td>{totalQty}</td>
            <td>-</td>
            <td>Rs. {totalValue}</td>
          </tr>
        </tfoot>
      </table>
    </div>
  );
};

export default OrderItemsTable;
