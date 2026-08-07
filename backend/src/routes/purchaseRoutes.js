import express from 'express';
import { Op } from 'sequelize';
import { Purchase, PurchaseItem, Product, Category, Supplier } from '../models/index.js';

const router = express.Router();

// GET /purchase/report
router.get('/', async (req, res) => {
  try {
    const { startDate, endDate, supplierId } = req.query;

    const purchaseWhere = {};
    if (startDate && endDate) {
      purchaseWhere.date = { [Op.between]: [new Date(startDate), new Date(endDate)] };
    }
    if (supplierId) {
      purchaseWhere.supplier_id = supplierId;
    }

    // Fetch matching purchase IDs
    const purchases = await Purchase.findAll({ where: purchaseWhere, attributes: ['id'] });
    const purchaseIds = purchases.map((p) => p.id);

    const purchaseItems = await PurchaseItem.findAll({
      where: { purchase_id: { [Op.in]: purchaseIds } },
      include: [
        {
          model: Purchase,
          as: 'Purchase',
          include: [{ model: Supplier, as: 'supplier', attributes: ['name'] }],
        },
        {
          model: Product,
          as: 'product',
          attributes: ['name', 'image'],
          include: [{ model: Category, as: 'Category', attributes: ['name'] }],
        },
      ],
    });

    const reportData = purchaseItems.map((item) => {
      const obj = item.toJSON();
      return {
        id: `${obj.purchase_id}-${obj.id}`,
        reference: obj.Purchase?.reference,
        sku: `PO-${obj.purchase_id}-${obj.id}`,
        dueDate: obj.Purchase?.date,
        product: {
          name: obj.product?.name || 'Unknown Product',
          image: obj.product?.image || '',
        },
        category: obj.product?.Category?.name || 'Uncategorized',
        purchaseQty: obj.quantity,
        purchaseAmount: obj.subtotal || obj.unitPrice * obj.quantity,
        stockQty: 0,
      };
    });

    res.json(reportData);
  } catch (error) {
    console.error('GET /purchase/report error:', error);
    res.status(500).json({ error: error.message });
  }
});

export default router;
