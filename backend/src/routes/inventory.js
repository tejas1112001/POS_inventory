import express from 'express';
import { Inventory, ProductVariant, Product, Store } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import { requirePermission } from '../middleware/permission.js';

const router = express.Router();

// GET /api/inventory — list all inventory records with product/variant/store details
router.get('/', authenticateToken, requirePermission('inventory', 'view'), async (req, res) => {
  try {
    const records = await Inventory.findAll({
      include: [
        {
          model: ProductVariant,
          as: 'Variant',
          attributes: ['id', 'sku', 'attributes', 'price'],
          include: [
            {
              model: Product,
              as: 'Product',
              attributes: ['id', 'name', 'image'],
            },
          ],
        },
        {
          model: Store,
          as: 'Store',
          attributes: ['id', 'name'],
        },
      ],
      order: [['updatedAt', 'DESC']],
    });

    const formatted = records.map((r) => {
      const json = r.toJSON();
      return {
        id: json.id,
        qty: json.qty,
        quantityAlert: json.quantityAlert,
        updatedAt: json.updatedAt,
        variantId: json.Variant?.id,
        sku: json.Variant?.sku,
        attributes: json.Variant?.attributes,
        price: json.Variant?.price,
        productId: json.Variant?.Product?.id,
        productName: json.Variant?.Product?.name,
        productImage: json.Variant?.Product?.image,
        storeId: json.Store?.id,
        storeName: json.Store?.name,
        status: json.qty <= 0 ? 'Out of Stock' : json.qty <= json.quantityAlert ? 'Low Stock' : 'In Stock',
      };
    });

    res.json(formatted);
  } catch (error) {
    console.error('GET /inventory error:', error);
    res.status(500).json({ error: error.message });
  }
});

// POST /api/inventory — upsert an inventory record
router.post('/', authenticateToken, requirePermission('inventory', 'edit'), async (req, res) => {
  try {
    const { variantId, storeId, qty, quantityAlert } = req.body;

    // Upsert: update if exists, create if not
    const [inventory] = await Inventory.upsert(
      { variant_id: variantId, store_id: storeId, qty, quantity_alert: quantityAlert },
      { returning: true }
    );

    res.status(201).json(inventory.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
