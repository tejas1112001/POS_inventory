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
import { requirePermission } from '../middleware/permission.js';

const router = express.Router();

// GET /sales-returns
router.get('/', authenticateToken, requirePermission('sales_returns', 'view'), async (req, res) => {
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
        payment_status: obj.payment_status,
        reference: obj.reference,
        customerName: obj.customer?.name ?? 'Walk-In Customer',
        customerEmail: obj.customer?.email ?? '',
        customerPhone: obj.customer?.phone ?? '',
        customerImage: obj.customer?.image ?? '',
        productName: firstProduct?.name ?? 'N/A',
        productImage: firstProduct?.image ?? '',
        itemsCount: obj.items?.length ?? 0,
        items: obj.items,
      };
    });

    res.json(transformed);
  } catch (error) {
    console.error('GET /sales-returns error:', error);
    res.status(500).json({ error: error.message });
  }
});

// POST /sales-returns
router.post('/', authenticateToken, requirePermission('sales_returns', 'create'), async (req, res) => {
  const t = await sequelize.transaction();
  try {
    const { returnItems, storeId, customerId, saleId, sale_id, ...saleReturnData } = req.body;
    const targetSaleId = saleId || sale_id;

    if (!storeId || !customerId || !Array.isArray(returnItems) || returnItems.length === 0) {
      throw new Error('Store, customer, and at least one return item are required.');
    }

    // 1. If saleId provided, validate original sale and item quantities
    if (targetSaleId) {
      const { Sale, OrderItem } = await import('../models/index.js');
      const originalSale = await Sale.findByPk(targetSaleId, {
        include: [{ model: OrderItem, as: 'items' }],
        transaction: t,
      });

      if (!originalSale) {
        throw new Error(`Original sale with ID ${targetSaleId} not found.`);
      }

      for (const item of returnItems) {
        const matchingOrderItem = originalSale.items?.find(
          (oi) =>
            oi.product_id === parseInt(item.productId) &&
            (!item.variantId || oi.variant_id === parseInt(item.variantId))
        );

        if (!matchingOrderItem) {
          throw new Error(`Product ${item.productId} was not part of original sale ID ${targetSaleId}.`);
        }

        if (item.quantity > matchingOrderItem.quantity) {
          throw new Error(
            `Return quantity (${item.quantity}) exceeds original sold quantity (${matchingOrderItem.quantity}) for product ${item.productId}.`
          );
        }
      }
    }

    // 2. Create the SaleReturn
    const computedTotal = parseFloat(saleReturnData.total) || returnItems.reduce((sum, i) => sum + (i.quantity * i.unitPrice), 0);
    const paid = parseFloat(saleReturnData.paid) || computedTotal;
    const due = Math.max(0, computedTotal - paid);

    const saleReturn = await SaleReturn.create(
      {
        ...saleReturnData,
        total: computedTotal,
        paid,
        due,
        sale_id: targetSaleId || null,
        customer_id: customerId,
        store_id: storeId,
        created_by: req.user.userId,
      },
      { transaction: t }
    );

    // 3. Create ReturnItem rows
    await ReturnItem.bulkCreate(
      returnItems.map((item) => ({
        sale_return_id: saleReturn.id,
        product_id: item.productId,
        variant_id: item.variantId || null,
        quantity: item.quantity,
        unitPrice: item.unitPrice,
        reason: item.reason || '',
        subtotal: item.total || (item.quantity * item.unitPrice),
      })),
      { transaction: t }
    );

    // 4. Restore inventory for each returned item
    for (const item of returnItems) {
      let variantId = item.variantId;

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
