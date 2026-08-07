import express from 'express';
import { Warehouse } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import isAdmin from '../middleware/admin.js';

const router = express.Router();

router.get('/', async (req, res) => {
  try {
    const warehouses = await Warehouse.findAll({ where: { status: 'Active' } });
    res.json(warehouses.map((w) => w.toJSON()));
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.post('/', authenticateToken, isAdmin, async (req, res) => {
  try {
    const { name, address, status } = req.body;
    const warehouse = await Warehouse.create({
      name,
      address,
      status: status || 'Active',
      created_by: req.user.userId,
    });
    res.status(201).json(warehouse.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
