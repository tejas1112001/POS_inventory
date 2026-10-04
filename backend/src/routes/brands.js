import express from 'express';
import { Brand, User } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import { requirePermission } from '../middleware/permission.js';

const router = express.Router();

const normalizeStatus = (s) => (['Active', 'Inactive'].includes(s) ? s : 'Active');

router.get('/', authenticateToken, requirePermission('brands', 'view'), async (req, res) => {
  try {
    const brands = await Brand.findAll({
      include: [{ model: User, as: 'creator', attributes: ['name'] }],
      order: [['createdAt', 'DESC']],
    });

    const formatted = brands.map((b) => ({
      ...b.toJSON(),
      createdBy: b.creator ? b.creator.name : 'Unknown',
      creator: undefined,
    }));

    res.json(formatted);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.post('/', authenticateToken, requirePermission('brands', 'create'), async (req, res) => {
  try {
    const { name, status, image } = req.body;
    if (!name || !name.trim()) {
      return res.status(400).json({ error: 'Brand name is required.' });
    }

    const brand = await Brand.create({
      name: name.trim(),
      status: normalizeStatus(status),
      image,
      created_by: req.user.userId,
    });

    res.status(201).json(brand.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.put('/:id', authenticateToken, requirePermission('brands', 'edit'), async (req, res) => {
  try {
    const brand = await Brand.findByPk(req.params.id);
    if (!brand) return res.status(404).json({ error: 'Brand not found.' });

    const { name, status, image } = req.body;
    if (!name || !name.trim()) {
      return res.status(400).json({ error: 'Brand name is required.' });
    }

    await brand.update({ name: name.trim(), status: normalizeStatus(status), image });
    res.json(brand.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.delete('/:id', authenticateToken, requirePermission('brands', 'delete'), async (req, res) => {
  try {
    const brand = await Brand.findByPk(req.params.id);
    if (!brand) return res.status(404).json({ error: 'Brand not found.' });
    await brand.destroy();
    res.status(204).send();
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
