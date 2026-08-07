import express from 'express';
import { ProductVariant } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';

const router = express.Router();

router.post('/', authenticateToken, async (req, res) => {
  try {
    const {
      productId, sku, itemBarcode, price, cost, weight,
      attributes, expiryDate, manufacturedDate,
    } = req.body;

    const variant = await ProductVariant.create({
      product_id: productId,
      sku,
      itemBarcode,
      price,
      cost,
      weight,
      attributes: attributes || {},
      expiryDate,
      manufacturedDate,
      created_by: req.user.userId,
    });

    res.status(201).json(variant.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
