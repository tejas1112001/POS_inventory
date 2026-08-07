import express from 'express';
import { Category, User } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import isAdmin from '../middleware/admin.js';

const router = express.Router();

const normalizeStatus = (s) => (['Active', 'Inactive'].includes(s) ? s : 'Active');

router.get('/', authenticateToken, async (req, res) => {
  try {
    const categories = await Category.findAll({
      include: [{ model: User, as: 'creator', attributes: ['name'] }],
      order: [['createdAt', 'DESC']],
    });

    const formatted = categories.map((c) => ({
      ...c.toJSON(),
      createdBy: c.creator ? c.creator.name : 'Unknown',
      creator: undefined,
    }));

    res.json(formatted);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.post('/', authenticateToken, isAdmin, async (req, res) => {
  try {
    const { name, status, image } = req.body;
    if (!name || !name.trim()) {
      return res.status(400).json({ error: 'Category name is required.' });
    }

    const category = await Category.create({
      name: name.trim(),
      status: normalizeStatus(status),
      image,
      created_by: req.user.userId,
    });

    res.status(201).json(category.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.put('/:id', authenticateToken, isAdmin, async (req, res) => {
  try {
    const category = await Category.findByPk(req.params.id);
    if (!category) return res.status(404).json({ error: 'Category not found.' });

    const { name, status, image } = req.body;
    if (!name || !name.trim()) {
      return res.status(400).json({ error: 'Category name is required.' });
    }

    await category.update({ name: name.trim(), status: normalizeStatus(status), image });
    res.json(category.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.delete('/:id', authenticateToken, isAdmin, async (req, res) => {
  try {
    const category = await Category.findByPk(req.params.id);
    if (!category) return res.status(404).json({ error: 'Category not found.' });
    await category.destroy();
    res.status(204).send();
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
