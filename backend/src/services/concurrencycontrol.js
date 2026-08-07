import { sequelize, Product, Sale, OrderItem, Customer, Inventory, Purchase, PurchaseItem } from '../models/index.js';
import { Op } from 'sequelize';

export class ConcurrencyControlService {

  // Optimistic locking for product updates
  static async updateProductWithLock(productId, updates, currentVersion) {
    const t = await sequelize.transaction();
    try {
      const product = await Product.findOne({
        where: { id: productId, version: currentVersion },
        lock: true,
        transaction: t,
      });

      if (!product) {
        throw new Error('Product not found or has been modified by another user');
      }

      await product.update({ ...updates, version: product.version + 1 }, { transaction: t });
      await t.commit();
      return product;
    } catch (error) {
      await t.rollback();
      throw error;
    }
  }

  // Atomic inventory check + sale creation
  static async processSaleWithInventoryCheck(saleData, items) {
    const t = await sequelize.transaction();
    try {
      const productUpdates = [];

      for (const item of items) {
        const inventory = await Inventory.findOne({
          where: { variant_id: item.variantId, store_id: item.storeId },
          lock: true,
          transaction: t,
        });

        if (!inventory) throw new Error(`Inventory not found for variant ${item.variantId}`);
        if (inventory.qty < item.quantity) {
          throw new Error(
            `Insufficient stock for variant ${item.variantId}. Available: ${inventory.qty}, Requested: ${item.quantity}`
          );
        }

        productUpdates.push({ inventory, newQty: inventory.qty - item.quantity });
      }

      const sale = await Sale.create({ ...saleData, date: new Date() }, { transaction: t });

      for (let i = 0; i < items.length; i++) {
        const item = items[i];
        const { inventory, newQty } = productUpdates[i];

        await OrderItem.create(
          {
            sale_id: sale.id,
            product_id: item.productId,
            variant_id: item.variantId,
            quantity: item.quantity,
            unit_price: item.unitPrice,
            discount: item.discount || 0,
            subtotal: item.subtotal,
          },
          { transaction: t }
        );

        await inventory.update({ qty: newQty }, { transaction: t });
      }

      await t.commit();
      return sale;
    } catch (error) {
      await t.rollback();
      throw error;
    }
  }

  // Atomic purchase + inventory increment
  static async processPurchaseWithInventoryUpdate(purchaseData, items) {
    const t = await sequelize.transaction();
    try {
      const purchase = await Purchase.create({ ...purchaseData, date: new Date() }, { transaction: t });

      for (const item of items) {
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

        await Inventory.increment('qty', {
          by: item.quantity,
          where: { variant_id: item.variantId, store_id: item.storeId },
          transaction: t,
        });
      }

      await t.commit();
      return purchase;
    } catch (error) {
      await t.rollback();
      throw error;
    }
  }

  // Get data changes since a given timestamp (for real-time sync)
  static async getDataChanges(lastSyncTimestamp) {
    const [products, sales] = await Promise.all([
      Product.findAll({
        where: { updatedAt: { [Op.gt]: lastSyncTimestamp } },
        attributes: ['id', 'name', 'updatedAt'],
      }),
      Sale.findAll({
        where: { updatedAt: { [Op.gt]: lastSyncTimestamp } },
        include: [
          { model: Customer, as: 'customer', attributes: ['name'] },
          { model: OrderItem, as: 'items' },
        ],
      }),
    ]);

    return { products, sales, lastSync: new Date() };
  }
}
