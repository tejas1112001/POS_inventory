import express from 'express';
import { SubCategory, Category } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import isAdmin from '../middleware/admin.js';

const router = express.Router();

router.get('/', async (req, res) => {
  try {
    const subCategories = await SubCategory.findAll({
      include: [{ model: Category, as: 'Category', attributes: ['name'] }],
    });
    res.json(subCategories.map((s) => s.toJSON()));
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.post('/', authenticateToken, isAdmin, async (req, res) => {
  try {
    const { name, status, categoryId } = req.body;
    const subCategory = await SubCategory.create({
      name,
      status: status || 'Active',
      category_id: categoryId,
      created_by: req.user.userId,
    });
    res.status(201).json(subCategory.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
