import express from 'express';
import { Op } from 'sequelize';
import { Sale, OrderItem, Product, Category, Brand } from '../models/index.js';

const router = express.Router();

// GET /sales-report
router.get('/', async (req, res) => {
  try {
    const { startDate, endDate, productId, customerId } = req.query;

    // Build sale filter
    const saleWhere = {};
    if (startDate && endDate) {
      saleWhere.date = { [Op.between]: [new Date(startDate), new Date(endDate)] };
    }
    if (customerId) {
      saleWhere.customer_id = customerId;
    }

    // Fetch matching sale IDs
    const sales = await Sale.findAll({ where: saleWhere, attributes: ['id'] });
    const saleIds = sales.map((s) => s.id);

    // Build order item filter
    const itemWhere = { sale_id: { [Op.in]: saleIds } };
    if (productId) {
      itemWhere.product_id = productId;
    }

    const orderItems = await OrderItem.findAll({
      where: itemWhere,
      include: [
        { model: Sale, as: 'sale', attributes: ['date'] },
        {
          model: Product,
          as: 'product',
          attributes: ['name', 'image'],
          include: [
            { model: Category, as: 'Category', attributes: ['name'] },
            { model: Brand, as: 'Brand', attributes: ['name'] },
          ],
        },
      ],
    });

    const reportData = orderItems.map((item) => {
      const obj = item.toJSON();
      return {
        id: `${obj.sale_id}-${obj.id}`,
        sku: `SO-${obj.sale_id}-${obj.id}`,
        date: obj.sale?.date,
        product: {
          name: obj.product?.name || 'Unknown Product',
          image: obj.product?.image || '',
          brand: obj.product?.Brand?.name || 'No Brand',
        },
        category: obj.product?.Category?.name || 'Uncategorized',
        soldQty: obj.quantity,
        soldAmount: obj.subtotal,
        stockQty: 0,
      };
    });

    res.json(reportData);
  } catch (error) {
    console.error('GET /sales-report error:', error);
    res.status(500).json({ error: error.message });
  }
});

export default router;
