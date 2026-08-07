import express from 'express';
import { Supplier, User } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import isAdmin from '../middleware/admin.js';

const router = express.Router();

const normalizeStatus = (s) => (['Active', 'Inactive'].includes(s) ? s : 'Active');

router.get('/', authenticateToken, async (req, res) => {
  try {
    const where = req.user?.role === 'admin' ? {} : { status: 'Active' };
    const suppliers = await Supplier.findAll({
      where,
      include: [{ model: User, as: 'creator', attributes: ['name'] }],
      order: [['createdAt', 'DESC']],
    });

    const formatted = suppliers.map((s) => ({
      ...s.toJSON(),
      createdBy: s.creator ? s.creator.name : 'Unknown',
      creator: undefined,
    }));

    res.json(formatted);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.post('/', authenticateToken, isAdmin, async (req, res) => {
  try {
    const { name, email, phone, address, image, status } = req.body;
    if (!name || !name.trim()) {
      return res.status(400).json({ error: 'Supplier name is required.' });
    }

    const supplier = await Supplier.create({
      name: name.trim(),
      email: email ? email.trim() : '',
      phone: phone ? phone.trim() : '',
      address: address ? address.trim() : '',
      image,
      status: normalizeStatus(status),
      created_by: req.user.userId,
    });

    res.status(201).json(supplier.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.put('/:id', authenticateToken, isAdmin, async (req, res) => {
  try {
    const supplier = await Supplier.findByPk(req.params.id);
    if (!supplier) return res.status(404).json({ error: 'Supplier not found.' });

    const { name, email, phone, address, image, status } = req.body;
    if (!name || !name.trim()) {
      return res.status(400).json({ error: 'Supplier name is required.' });
    }

    await supplier.update({
      name: name.trim(),
      email: email ? email.trim() : '',
      phone: phone ? phone.trim() : '',
      address: address ? address.trim() : '',
      image,
      status: normalizeStatus(status),
    });

    res.json(supplier.toJSON());
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.delete('/:id', authenticateToken, isAdmin, async (req, res) => {
  try {
    const supplier = await Supplier.findByPk(req.params.id);
    if (!supplier) return res.status(404).json({ error: 'Supplier not found.' });
    await supplier.destroy();
    res.status(204).send();
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
