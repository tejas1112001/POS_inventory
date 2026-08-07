import express from 'express';
import { Op } from 'sequelize';
import { User } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import isAdmin from '../middleware/admin.js';

const router = express.Router();

router.get('/', authenticateToken, async (req, res) => {
  try {
    const where = { role: 'biller' };

    if (req.user.role !== 'admin') {
      where.id = req.user.userId;
    }

    const { q, status } = req.query;
    if (status && ['Active', 'Inactive'].includes(status)) {
      where.status = status;
    }

    if (q && q.trim()) {
      where[Op.or] = [
        { name: { [Op.iLike]: `%${q.trim()}%` } },
        { email: { [Op.iLike]: `%${q.trim()}%` } },
      ];
    }

    const billers = await User.findAll({
      where,
      attributes: { exclude: ['password'] },
      order: [['createdAt', 'DESC']],
    });

    res.json(billers.map((b) => b.toJSON()));
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.put('/:id', authenticateToken, isAdmin, async (req, res) => {
  try {
    const biller = await User.findByPk(req.params.id);
    if (!biller) return res.status(404).json({ error: 'Biller not found.' });

    const { name, email, role, status } = req.body;
    if (!name || !name.trim()) {
      return res.status(400).json({ error: 'Biller name is required.' });
    }

    const nextEmail = email ? email.trim() : '';
    if (nextEmail) {
      const existing = await User.findOne({
        where: { email: nextEmail, id: { [Op.ne]: req.params.id } },
      });
      if (existing) return res.status(409).json({ error: 'Email is already in use.' });
    }

    await biller.update({
      name: name.trim(),
      email: nextEmail,
      role: ['admin', 'biller'].includes(role) ? role : 'biller',
      status: ['Active', 'Inactive'].includes(status) ? status : 'Active',
    });

    const result = biller.toJSON();
    delete result.password;
    res.json(result);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.delete('/:id', authenticateToken, isAdmin, async (req, res) => {
  try {
    const biller = await User.findByPk(req.params.id);
    if (!biller) return res.status(404).json({ error: 'Biller not found.' });
    await biller.destroy();
    res.status(204).send();
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
