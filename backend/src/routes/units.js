import express from 'express';
import { Unit, User } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import isAdmin from '../middleware/admin.js';

const router = express.Router();

const normalizeStatus = (s) => (['Active', 'Inactive'].includes(s) ? s : 'Active');

router.get('/', authenticateToken, async (req, res) => {
  try {
    const units = await Unit.findAll({
      include: [{ model: User, as: 'creator', attributes: ['name'] }],
      order: [['createdAt', 'DESC']],
    });

    const formatted = units.map((u) => ({
      ...u.toJSON(),
      createdBy: u.creator ? u.creator.name : 'Unknown',
      creator: undefined,
    }));

    res.json(formatted);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.post('/', authenticateToken, isAdmin, async (req, res) => {
  try {
    const { name, short_name, status } = req.body;
    if (!name || !name.trim()) {
      return res.status(400).json({ error: 'Unit name is required.' });
    }

    const unit = await Unit.create({
      name: name.trim(),
      short_name: short_name ? short_name.trim() : '',
      status: normalizeStatus(status),
      created_by: req.user.userId,
    });

    res.status(201).json(unit.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.put('/:id', authenticateToken, isAdmin, async (req, res) => {
  try {
    const unit = await Unit.findByPk(req.params.id);
    if (!unit) return res.status(404).json({ error: 'Unit not found.' });

    const { name, short_name, status } = req.body;
    if (!name || !name.trim()) {
      return res.status(400).json({ error: 'Unit name is required.' });
    }

    await unit.update({
      name: name.trim(),
      short_name: short_name ? short_name.trim() : '',
      status: normalizeStatus(status),
    });

    res.json(unit.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.delete('/:id', authenticateToken, isAdmin, async (req, res) => {
  try {
    const unit = await Unit.findByPk(req.params.id);
    if (!unit) return res.status(404).json({ error: 'Unit not found.' });
    await unit.destroy();
    res.status(204).send();
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
