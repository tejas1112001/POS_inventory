import express from 'express';
import {
  sequelize,
  SaleReturn,
  ReturnItem,
  Inventory,
  ProductVariant,
  Customer,
  Product,
} from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import isAdmin from '../middleware/admin.js';

const router = express.Router();

// GET /sales-returns
router.get('/', async (req, res) => {
  try {
    const salesReturns = await SaleReturn.findAll({
      include: [
        {
          model: Customer,
          as: 'customer',
          attributes: ['id', 'name', 'email', 'phone', 'image'],
        },
        {
          model: ReturnItem,
          as: 'items',
          include: [
            {
              model: Product,
              as: 'product',
              attributes: ['id', 'name', 'image'],
            },
          ],
        },
      ],
      order: [['createdAt', 'DESC']],
    });

    const transformed = salesReturns.map((r) => {
      const obj = r.toJSON();
      const firstItem = obj.items?.[0];
      const firstProduct = firstItem?.product ?? null;

      return {
        id: obj.id,
        date: obj.date,
        status: obj.status,
        total: obj.total,
        paid: obj.paid,
        due: obj.due,
        paymentStatus: obj.payment_status,
        customer: obj.customer
          ? {
              id: obj.customer.id,
              name: obj.customer.name,
              email: obj.customer.email,
              phone: obj.customer.phone,
              avatar: obj.customer.image,
            }
          : null,
        product: firstProduct
          ? { id: firstProduct.id, name: firstProduct.name, image: firstProduct.image }
          : null,
      };
    });

    res.json(transformed);
  } catch (error) {
    console.error('GET /sales-returns error:', error);
    res.status(500).json({ error: error.message });
  }
});

// POST /sales-returns
router.post('/', authenticateToken, isAdmin, async (req, res) => {
  const t = await sequelize.transaction();
  try {
    const { returnItems, storeId, customerId, ...saleReturnData } = req.body;

    if (!storeId || !customerId || !Array.isArray(returnItems) || returnItems.length === 0) {
      throw new Error('Store, customer, and at least one return item are required.');
    }

    // 1. Create the SaleReturn
    const saleReturn = await SaleReturn.create(
      {
        ...saleReturnData,
        customer_id: customerId,
        store_id: storeId,
        created_by: req.user.userId,
      },
      { transaction: t }
    );

    // 2. Create ReturnItem rows
    await ReturnItem.bulkCreate(
      returnItems.map((item) => ({
        sale_return_id: saleReturn.id,
        product_id: item.productId,
        variant_id: item.variantId || null,
        quantity: item.quantity,
        unitPrice: item.unitPrice,
        reason: item.reason || '',
        subtotal: item.total,
      })),
      { transaction: t }
    );

    // 3. Restore inventory for each returned item
    for (const item of returnItems) {
      let variantId = item.variantId;

      // Fall back to first variant of product if no variantId provided
      if (!variantId && item.productId) {
        const variant = await ProductVariant.findOne({
          where: { product_id: item.productId },
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

    await t.commit();
    res.status(201).json(saleReturn.toJSON());
  } catch (error) {
    await t.rollback();
    console.error('POST /sales-returns error:', error);
    res.status(500).json({ error: error.message });
  }
});

export default router;
