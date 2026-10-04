import express from 'express';
import { sequelize, Sale, OrderItem, Inventory, Product, ProductVariant, Customer, User } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import { requirePermission } from '../middleware/permission.js';

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

import { Op } from 'sequelize';

// GET /sales (Supports pagination, search, date filtering)
router.get('/', authenticateToken, requirePermission('sales', 'view'), async (req, res) => {
  try {
    const { page, limit, search, startDate, endDate, storeId, customerId } = req.query;
    const where = {};

    if (req.user.role === 'biller') {
      where.user_id = req.user.userId;
    }
    if (storeId) where.store_id = storeId;
    if (customerId) where.customer_id = customerId;
    if (search && search.trim()) {
      where.reference = { [Op.iLike || Op.like]: `%${search.trim()}%` };
    }
    if (startDate || endDate) {
      where.createdAt = {};
      if (startDate) where.createdAt[Op.gte] = new Date(startDate);
      if (endDate) where.createdAt[Op.lte] = new Date(endDate);
    }

    if (page || limit) {
      const pageNum = Math.max(1, parseInt(page) || 1);
      const limitNum = Math.max(1, parseInt(limit) || 20);
      const offset = (pageNum - 1) * limitNum;

      const { count, rows } = await Sale.findAndCountAll({
        where,
        include: saleIncludes,
        limit: limitNum,
        offset,
        order: [['createdAt', 'DESC']],
        distinct: true,
      });

      return res.json({
        data: rows.map((s) => s.toJSON()),
        pagination: {
          total: count,
          page: pageNum,
          limit: limitNum,
          totalPages: Math.ceil(count / limitNum),
        },
      });
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
router.post('/', authenticateToken, requirePermission('sales', 'create'), async (req, res) => {
  const t = await sequelize.transaction();
  try {
    const { orderItems, customerId, storeId, reference, ...saleData } = req.body;

    if (!customerId || !storeId || !Array.isArray(orderItems) || orderItems.length === 0) {
      throw new Error('Customer, store, and at least one order item are required.');
    }

    // Server-side reference generation if not provided
    const saleReference = reference || `POS-INV-${Date.now()}-${Math.floor(1000 + Math.random() * 9000)}`;

    // Validate quantities and compute items total
    let calculatedSubtotal = 0;
    for (const item of orderItems) {
      const qty = parseInt(item.quantity);
      if (isNaN(qty) || qty <= 0) {
        throw new Error(`Invalid item quantity: ${item.quantity}`);
      }
      const unitPrice = parseFloat(item.unitPrice) || 0;
      calculatedSubtotal += qty * unitPrice;
    }

    const discount = parseFloat(saleData.discount) || 0;
    const tax = parseFloat(saleData.tax) || 0;
    const shipping = parseFloat(saleData.shipping) || 0;
    const computedTotal = Math.max(0, calculatedSubtotal - discount + tax + shipping);
    const paid = parseFloat(saleData.paid) || computedTotal;
    const due = Math.max(0, computedTotal - paid);
    const paymentStatus = paid >= computedTotal ? 'Paid' : paid > 0 ? 'Partial' : 'Unpaid';

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
      {
        ...saleData,
        reference: saleReference,
        subtotal: calculatedSubtotal,
        discount,
        tax,
        shipping,
        total: computedTotal,
        paid,
        due,
        payment_status: saleData.payment_status || paymentStatus,
        customer_id: customerId,
        store_id: storeId,
        user_id: req.user.userId,
      },
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
        subtotal: item.total || (item.quantity * item.unitPrice),
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
    const json = populated.toJSON();
    // Normalize item fields for receipts/clients
    json.items = (json.items || []).map((item) => ({
      ...item,
      quantity: item.quantity,
      qty: item.quantity,
      unitPrice: item.unit_price,
      price: item.unit_price,
    }));

    res.status(201).json(json);
  } catch (error) {
    await t.rollback();
    console.error('POST /sales error:', error);
    res.status(500).json({ error: error.message });
  }
});

// PUT /sales/:id
router.put('/:id', authenticateToken, requirePermission('sales', 'edit'), async (req, res) => {
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
router.delete('/:id', authenticateToken, requirePermission('sales', 'delete'), async (req, res) => {
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
