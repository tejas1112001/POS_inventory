import express from 'express';
import { sequelize, Purchase, PurchaseItem, Inventory, Supplier, Product } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import { requirePermission } from '../middleware/permission.js';

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
router.get('/', authenticateToken, requirePermission('purchases', 'view'), async (req, res) => {
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
router.post('/', authenticateToken, requirePermission('purchases', 'create'), async (req, res) => {
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

        // Update or create inventory ONLY if status is 'Received'
        if (status === 'Received' && item.variantId && item.storeId) {
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

// PUT /purchases/:id — update status / payment_status
router.put('/:id', authenticateToken, requirePermission('purchases', 'edit'), async (req, res) => {
  const t = await sequelize.transaction();
  try {
    const purchase = await Purchase.findByPk(req.params.id, {
      include: [{ model: PurchaseItem, as: 'items' }],
      transaction: t,
    });
    if (!purchase) throw new Error('Purchase not found');

    const oldStatus = purchase.status;
    const { status, payment_status, paid, due } = req.body;

    const updates = {};
    if (status !== undefined) updates.status = status;
    if (payment_status !== undefined) updates.payment_status = payment_status;
    if (paid !== undefined) updates.paid = parseFloat(paid) || 0;
    if (due !== undefined) updates.due = parseFloat(due) || 0;

    await purchase.update(updates, { transaction: t });

    // Handle stock increment if status changes to 'Received'
    if (oldStatus !== 'Received' && status === 'Received' && purchase.items) {
      const storeId = req.body.storeId || 1;
      for (const item of purchase.items) {
        let variantId = item.variant_id;
        if (!variantId && item.product_id) {
          const { ProductVariant } = await import('../models/index.js');
          const variant = await ProductVariant.findOne({
            where: { product_id: item.product_id },
            transaction: t,
          });
          if (variant) variantId = variant.id;
        }

        if (variantId && storeId) {
          const existing = await Inventory.findOne({
            where: { variant_id: variantId, store_id: storeId },
            transaction: t,
          });

          if (existing) {
            await existing.increment('qty', { by: item.quantity, transaction: t });
          } else {
            await Inventory.create(
              { variant_id: variantId, store_id: storeId, qty: item.quantity, quantity_alert: 0 },
              { transaction: t }
            );
          }
        }
      }
    }

    await t.commit();
    const updated = await Purchase.findByPk(purchase.id, { include: purchaseIncludes });
    res.json(updated.toJSON());
  } catch (error) {
    await t.rollback();
    console.error('PUT /purchases/:id error:', error);
    res.status(500).json({ error: error.message });
  }
});

export default router;
