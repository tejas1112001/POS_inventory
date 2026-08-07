import express from 'express';
import { Sale, Customer, OrderItem, Product } from '../models/index.js';

const router = express.Router();

// GET /invoices — derives invoices from sales
router.get('/', async (req, res) => {
  try {
    const sales = await Sale.findAll({
      include: [
        { model: Customer, as: 'customer', attributes: ['name', 'email', 'phone', 'image'] },
        {
          model: OrderItem,
          as: 'items',
          include: [{ model: Product, as: 'product' }],
        },
      ],
      order: [['createdAt', 'DESC']],
    });

    const invoices = sales.map((sale) => {
      const obj = sale.toJSON();
      return {
        id: obj.id,
        invoiceNo: obj.reference,
        customer: obj.customer
          ? { name: obj.customer.name, avatar: obj.customer.image }
          : { name: 'N/A', avatar: '' },
        dueDate: obj.date
          ? new Date(obj.date).toLocaleDateString('en-GB', {
              day: '2-digit',
              month: 'short',
              year: 'numeric',
            })
          : '',
        amount: obj.total,
        paid: obj.paid,
        amountDue: obj.due,
        status: obj.payment_status,
      };
    });

    res.json(invoices);
  } catch (error) {
    console.error('GET /invoices error:', error);
    res.status(500).json({ error: error.message });
  }
});

export default router;
