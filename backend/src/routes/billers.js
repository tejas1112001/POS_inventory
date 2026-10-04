import express from 'express';
import { Op } from 'sequelize';
import { User, Role } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import { requirePermission } from '../middleware/permission.js';

const router = express.Router();

router.get('/', authenticateToken, requirePermission('users', 'view'), async (req, res) => {
  try {
    const where = {};
    const { q, status } = req.query;
    if (status && ['Active', 'Inactive'].includes(status)) {
      where.status = status;
    }

    if (q && q.trim()) {
      where[Op.or] = [
        { name: { [Op.iLike || Op.like]: `%${q.trim()}%` } },
        { email: { [Op.iLike || Op.like]: `%${q.trim()}%` } },
      ];
    }

    const users = await User.findAll({
      where,
      attributes: { exclude: ['password'] },
      include: [{ model: Role, as: 'Role', attributes: ['id', 'name'] }],
      order: [['createdAt', 'DESC']],
    });

    res.json(users.map((b) => b.toJSON()));
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.put('/:id', authenticateToken, requirePermission('users', 'edit'), async (req, res) => {
  try {
    const biller = await User.findByPk(req.params.id);
    if (!biller) return res.status(404).json({ error: 'User not found.' });

    const { name, email, role, role_id, status } = req.body;
    if (!name || !name.trim()) {
      return res.status(400).json({ error: 'User name is required.' });
    }

    const nextEmail = email ? email.trim() : '';
    if (nextEmail) {
      const existing = await User.findOne({
        where: { email: nextEmail, id: { [Op.ne]: req.params.id } },
      });
      if (existing) return res.status(409).json({ error: 'Email is already in use.' });
    }

    const updates = {
      name: name.trim(),
      email: nextEmail,
      status: ['Active', 'Inactive'].includes(status) ? status : 'Active',
    };
    if (role) updates.role = role;
    if (role_id !== undefined) updates.role_id = role_id;

    await biller.update(updates);

    const result = biller.toJSON();
    delete result.password;
    res.json(result);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.delete('/:id', authenticateToken, requirePermission('users', 'delete'), async (req, res) => {
  try {
    const biller = await User.findByPk(req.params.id);
    if (!biller) return res.status(404).json({ error: 'User not found.' });
    await biller.destroy();
    res.status(204).send();
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
