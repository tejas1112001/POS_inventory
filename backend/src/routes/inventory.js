import express from 'express';
import { Inventory } from '../models/index.js';

const router = express.Router();

router.post('/', async (req, res) => {
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
