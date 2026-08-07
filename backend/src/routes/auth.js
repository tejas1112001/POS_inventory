import express from 'express';
import bcrypt from 'bcrypt';
import jwt from 'jsonwebtoken';
import { User } from '../models/index.js';

const router = express.Router();

router.post('/login', async (req, res) => {
  try {
    const { email, password } = req.body;
    if (!email || !password) {
      return res.status(400).json({ error: 'Email and password are required' });
    }

    const secret = process.env.JWT_SECRET;
    if (!secret) {
      return res.status(500).json({ error: 'JWT secret is not configured' });
    }

    const user = await User.findOne({ where: { email } });
    if (!user) return res.status(404).json({ error: 'User not found' });

    const validPassword = await bcrypt.compare(password, user.password);
    if (!validPassword) return res.status(401).json({ error: 'Invalid password' });

    const token = jwt.sign({ userId: user.id, role: user.role }, secret, { expiresIn: '1h' });
    const userJson = user.toJSON();
    delete userJson.password;
    res.json({ user: userJson, token });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.post('/signup', async (req, res) => {
  try {
    const { email, password, ...rest } = req.body;
    if (!email || !password) {
      return res.status(400).json({ error: 'Email and password are required' });
    }

    const secret = process.env.JWT_SECRET;
    if (!secret) {
      return res.status(500).json({ error: 'JWT secret is not configured' });
    }

    const hashedPassword = await bcrypt.hash(password, 10);
    const user = await User.create({ ...rest, email, password: hashedPassword });
    const token = jwt.sign({ userId: user.id, role: user.role }, secret, { expiresIn: '1h' });
    const userJson = user.toJSON();
    delete userJson.password;
    res.status(201).json({ user: userJson, token });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
