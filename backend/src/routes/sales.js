import express from 'express';
import { sequelize, Sale, OrderItem, Inventory, Product, ProductVariant, Customer, User } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import isAdmin from '../middleware/admin.js';

const router = express.Router();

const saleIncludes = [
  { model: Customer, as: 'customer', attributes: ['name'] },
  { model: User, as: 'user', attributes: ['name'] },
  {
    model: OrderItem,
    as: 'items',
    include: [
      { model: Product, as: 'product', attributes: ['name'] },
      { model: ProductVariant, as: 'variant', attributes: ['sku', 'attributes'] },
    ],
  },
];

// GET /sales
router.get('/', authenticateToken, async (req, res) => {
  try {
    const where = {};
    if (req.user.role === 'biller') {
      where.user_id = req.user.userId;
    }

    const sales = await Sale.findAll({
      where,
      include: saleIncludes,
      order: [['createdAt', 'DESC']],
    });

    res.json(sales.map((s) => s.toJSON()));
  } catch (error) {
    console.error('GET /sales error:', error);
    res.status(500).json({ error: error.message });
  }
});

// POST /sales
router.post('/', authenticateToken, async (req, res) => {
  const t = await sequelize.transaction();
  try {
    const { orderItems, customerId, storeId, ...saleData } = req.body;

    if (!customerId || !storeId || !Array.isArray(orderItems) || orderItems.length === 0) {
      throw new Error('Customer, store, and at least one order item are required.');
    }

    // 1. Validate inventory
    for (const item of orderItems) {
      const inventory = await Inventory.findOne({
        where: { variant_id: item.variantId, store_id: storeId },
        transaction: t,
      });
      if (!inventory || inventory.qty < item.quantity) {
        throw new Error(
          `Insufficient inventory for variant ${item.variantId}. Available: ${inventory?.qty || 0}, Requested: ${item.quantity}`
        );
      }
    }

    // 2. Create sale
    const sale = await Sale.create(
      { ...saleData, customer_id: customerId, store_id: storeId, user_id: req.user.userId },
      { transaction: t }
    );

    // 3. Create order items
    await OrderItem.bulkCreate(
      orderItems.map((item) => ({
        sale_id: sale.id,
        product_id: item.productId,
        variant_id: item.variantId,
        quantity: item.quantity,
        unit_price: item.unitPrice,
        discount: item.discount || 0,
        subtotal: item.total,
      })),
      { transaction: t }
    );

    // 4. Decrement inventory
    for (const item of orderItems) {
      await Inventory.decrement('qty', {
        by: item.quantity,
        where: { variant_id: item.variantId, store_id: storeId },
        transaction: t,
      });
    }

    await t.commit();

    const populated = await Sale.findByPk(sale.id, { include: saleIncludes });
    res.status(201).json(populated.toJSON());
  } catch (error) {
    await t.rollback();
    console.error('POST /sales error:', error);
    res.status(500).json({ error: error.message });
  }
});

// PUT /sales/:id
router.put('/:id', authenticateToken, isAdmin, async (req, res) => {
  const t = await sequelize.transaction();
  try {
    const saleId = req.params.id;
    const { orderItems, customerId, storeId, ...saleData } = req.body;

    if (!customerId || !storeId || !Array.isArray(orderItems) || orderItems.length === 0) {
      throw new Error('Customer, store, and at least one order item are required.');
    }

    const originalSale = await Sale.findByPk(saleId, {
      include: [{ model: OrderItem, as: 'items' }],
      transaction: t,
    });
    if (!originalSale) throw new Error('Sale not found');

    // 1. Restore inventory from original items
    for (const item of originalSale.items) {
      await Inventory.increment('qty', {
        by: item.quantity,
        where: { variant_id: item.variant_id, store_id: originalSale.store_id },
        transaction: t,
      });
    }

    // 2. Validate new inventory
    for (const item of orderItems) {
      const inventory = await Inventory.findOne({
        where: { variant_id: item.variantId, store_id: storeId },
        transaction: t,
      });
      if (!inventory || inventory.qty < item.quantity) {
        throw new Error(
          `Insufficient inventory for variant ${item.variantId}. Available: ${inventory?.qty || 0}, Requested: ${item.quantity}`
        );
      }
    }

    // 3. Delete old order items
    await OrderItem.destroy({ where: { sale_id: saleId }, transaction: t });

    // 4. Create new order items
    await OrderItem.bulkCreate(
      orderItems.map((item) => ({
        sale_id: saleId,
        product_id: item.productId,
        variant_id: item.variantId,
        quantity: item.quantity,
        unit_price: item.unitPrice,
        discount: item.discount || 0,
        subtotal: item.total,
      })),
      { transaction: t }
    );

    // 5. Update sale
    await originalSale.update(
      { ...saleData, customer_id: customerId, store_id: storeId, user_id: req.user.userId },
      { transaction: t }
    );

    // 6. Decrement new inventory
    for (const item of orderItems) {
      await Inventory.decrement('qty', {
        by: item.quantity,
        where: { variant_id: item.variantId, store_id: storeId },
        transaction: t,
      });
    }

    await t.commit();

    const populated = await Sale.findByPk(saleId, { include: saleIncludes });
    res.status(200).json(populated.toJSON());
  } catch (error) {
    await t.rollback();
    console.error('PUT /sales/:id error:', error);
    res.status(500).json({ error: error.message });
  }
});

// DELETE /sales/:id
router.delete('/:id', authenticateToken, isAdmin, async (req, res) => {
  const t = await sequelize.transaction();
  try {
    const sale = await Sale.findByPk(req.params.id, {
      include: [{ model: OrderItem, as: 'items' }],
      transaction: t,
    });
    if (!sale) throw new Error('Sale not found');

    // Restore inventory
    for (const item of sale.items) {
      await Inventory.increment('qty', {
        by: item.quantity,
        where: { variant_id: item.variant_id, store_id: sale.store_id },
        transaction: t,
      });
    }

    await OrderItem.destroy({ where: { sale_id: sale.id }, transaction: t });
    await sale.destroy({ transaction: t });

    await t.commit();
    res.status(204).send();
  } catch (error) {
    await t.rollback();
    console.error('DELETE /sales/:id error:', error);
    res.status(500).json({ error: error.message });
  }
});

export default router;
