import express from 'express';
import { sequelize, Purchase, PurchaseItem, Inventory, Supplier, Product } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import isAdmin from '../middleware/admin.js';

const router = express.Router();

const purchaseIncludes = [
  { model: Supplier, as: 'supplier', attributes: ['name', 'email', 'phone'] },
  {
    model: PurchaseItem,
    as: 'items',
    include: [{ model: Product, as: 'product', attributes: ['name'] }],
  },
];

// GET /purchases
router.get('/', async (req, res) => {
  try {
    const purchases = await Purchase.findAll({
      include: purchaseIncludes,
      order: [['date', 'DESC']],
    });
    res.json(purchases.map((p) => p.toJSON()));
  } catch (error) {
    console.error('GET /purchases error:', error);
    res.status(500).json({ error: error.message });
  }
});

// POST /purchases
router.post('/', authenticateToken, isAdmin, async (req, res) => {
  const t = await sequelize.transaction();
  try {
    const { reference, date, status, payment_status, total, paid, due, supplierId, purchaseItems } = req.body;

    const purchase = await Purchase.create(
      {
        reference,
        date: date || new Date(),
        status,
        payment_status,
        total: parseFloat(total) || 0,
        paid: parseFloat(paid) || 0,
        due: parseFloat(due) || 0,
        supplier_id: supplierId,
        created_by: req.user.userId,
      },
      { transaction: t }
    );

    if (Array.isArray(purchaseItems) && purchaseItems.length > 0) {
      for (const item of purchaseItems) {
        await PurchaseItem.create(
          {
            purchase_id: purchase.id,
            product_id: item.productId,
            quantity: item.quantity,
            unitPrice: item.unitPrice,
            discount: item.discount || 0,
            subtotal: item.subtotal,
          },
          { transaction: t }
        );

        // Update or create inventory
        if (item.variantId && item.storeId) {
          const existing = await Inventory.findOne({
            where: { variant_id: item.variantId, store_id: item.storeId },
            transaction: t,
          });

          if (existing) {
            await existing.increment('qty', { by: item.quantity, transaction: t });
          } else {
            await Inventory.create(
              { variant_id: item.variantId, store_id: item.storeId, qty: item.quantity, quantity_alert: 0 },
              { transaction: t }
            );
          }
        }
      }
    }

    await t.commit();

    const created = await Purchase.findByPk(purchase.id, { include: purchaseIncludes });
    res.status(201).json(created.toJSON());
  } catch (error) {
    await t.rollback();
    console.error('POST /purchases error:', error);
    res.status(500).json({ error: error.message });
  }
});

export default router;
