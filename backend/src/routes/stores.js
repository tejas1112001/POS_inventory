import express from 'express';
import { Store } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import { requirePermission } from '../middleware/permission.js';

const router = express.Router();

router.get('/', authenticateToken, async (req, res) => {
  try {
    const stores = await Store.findAll({ where: { status: 'Active' } });
    res.json(stores.map((s) => s.toJSON()));
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.get('/:id', authenticateToken, async (req, res) => {
  try {
    const store = await Store.findByPk(req.params.id);
    if (!store) return res.status(404).json({ error: 'Store not found' });
    res.json(store.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.post('/', authenticateToken, requirePermission('stores', 'create'), async (req, res) => {
  try {
    const { name, address, status } = req.body;
    const store = await Store.create({
      name,
      address,
      status: status || 'Active',
      created_by: req.user.userId,
    });
    res.status(201).json(store.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.put('/:id', authenticateToken, requirePermission('stores', 'edit'), async (req, res) => {
  try {
    const store = await Store.findByPk(req.params.id);
    if (!store) return res.status(404).json({ error: 'Store not found' });
    const { name, address, status } = req.body;
    await store.update({ name, address, status });
    res.json(store.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.delete('/:id', authenticateToken, requirePermission('stores', 'delete'), async (req, res) => {
  try {
    const store = await Store.findByPk(req.params.id);
    if (!store) return res.status(404).json({ error: 'Store not found' });
    await store.destroy();
    res.json({ message: 'Store deleted successfully' });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
