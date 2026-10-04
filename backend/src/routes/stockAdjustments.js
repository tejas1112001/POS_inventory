import express from 'express';
import { sequelize, StockAdjustment, Inventory, ProductVariant, Product, Store, User } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import { requirePermission } from '../middleware/permission.js';

const router = express.Router();

// GET /api/stock-adjustments
router.get('/', authenticateToken, requirePermission('stock_adjustments', 'view'), async (req, res) => {
  try {
    const { page, limit } = req.query;
    const includes = [
      { model: Store, as: 'Store', attributes: ['id', 'name'] },
      { model: User, as: 'creator', attributes: ['id', 'name'] },
      {
        model: ProductVariant,
        as: 'Variant',
        include: [{ model: Product, as: 'Product', attributes: ['id', 'name'] }],
      },
    ];

    if (page || limit) {
      const pageNum = Math.max(1, parseInt(page) || 1);
      const limitNum = Math.max(1, parseInt(limit) || 20);
      const offset = (pageNum - 1) * limitNum;

      const { count, rows } = await StockAdjustment.findAndCountAll({
        include: includes,
        limit: limitNum,
        offset,
        order: [['createdAt', 'DESC']],
        distinct: true,
      });

      return res.json({
        data: rows.map((r) => r.toJSON()),
        pagination: {
          total: count,
          page: pageNum,
          limit: limitNum,
          totalPages: Math.ceil(count / limitNum),
        },
      });
    }

    const records = await StockAdjustment.findAll({
      include: includes,
      order: [['createdAt', 'DESC']],
    });
    res.json(records.map((r) => r.toJSON()));
  } catch (error) {
    console.error('GET /api/stock-adjustments error:', error);
    res.status(500).json({ error: error.message });
  }
});

// POST /api/stock-adjustments
router.post('/', authenticateToken, requirePermission('stock_adjustments', 'create'), async (req, res) => {
  const t = await sequelize.transaction();
  try {
    const { variantId, storeId, type, quantity, reason, note } = req.body;

    const qty = parseInt(quantity);
    if (!variantId || !storeId || !type || isNaN(qty) || qty <= 0) {
      throw new Error('variantId, storeId, valid type (Addition/Subtraction), and positive quantity are required.');
    }

    if (!['Addition', 'Subtraction'].includes(type)) {
      throw new Error('Type must be either Addition or Subtraction.');
    }

    const inventory = await Inventory.findOne({
      where: { variant_id: variantId, store_id: storeId },
      transaction: t,
    });

    if (type === 'Subtraction') {
      const currentQty = inventory ? inventory.qty : 0;
      if (currentQty < qty) {
        throw new Error(`Cannot subtract ${qty} units. Current stock is ${currentQty}.`);
      }
      if (inventory) {
        await inventory.decrement('qty', { by: qty, transaction: t });
      }
    } else {
      if (inventory) {
        await inventory.increment('qty', { by: qty, transaction: t });
      } else {
        await Inventory.create(
          { variant_id: variantId, store_id: storeId, qty, quantity_alert: 0 },
          { transaction: t }
        );
      }
    }

    const adjustment = await StockAdjustment.create(
      {
        variant_id: variantId,
        store_id: storeId,
        type,
        quantity: qty,
        reason: reason || 'Manual Adjustment',
        note: note || '',
        created_by: req.user.userId,
      },
      { transaction: t }
    );

    await t.commit();
    res.status(201).json(adjustment.toJSON());
  } catch (error) {
    await t.rollback();
    console.error('POST /api/stock-adjustments error:', error);
    res.status(500).json({ error: error.message });
  }
});

export default router;
