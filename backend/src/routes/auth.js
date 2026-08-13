import express from 'express';
import bcrypt from 'bcrypt';
import jwt from 'jsonwebtoken';
import { User } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';

const router = express.Router();

const REMEMBER_EXPIRY = '30d';
const SESSION_EXPIRY = '12h';

function signToken(user, remember = true) {
  const secret = process.env.JWT_SECRET;
  return jwt.sign(
    { userId: user.id, role: user.role },
    secret,
    { expiresIn: remember ? REMEMBER_EXPIRY : SESSION_EXPIRY }
  );
}

function sanitizeUser(user) {
  const userJson = user.toJSON();
  delete userJson.password;
  return userJson;
}

router.post('/login', async (req, res) => {
  try {
    const { email, password, remember = true } = req.body;
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

    const token = signToken(user, remember);
    res.json({ user: sanitizeUser(user), token, remember });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.post('/signup', async (req, res) => {
  try {
    const { email, password, remember = true, ...rest } = req.body;
    if (!email || !password) {
      return res.status(400).json({ error: 'Email and password are required' });
    }

    const secret = process.env.JWT_SECRET;
    if (!secret) {
      return res.status(500).json({ error: 'JWT secret is not configured' });
    }

    const hashedPassword = await bcrypt.hash(password, 10);
    const user = await User.create({ ...rest, email, password: hashedPassword });
    const token = signToken(user, remember);
    res.status(201).json({ user: sanitizeUser(user), token, remember });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.post('/refresh', authenticateToken, async (req, res) => {
  try {
    const secret = process.env.JWT_SECRET;
    if (!secret) {
      return res.status(500).json({ error: 'JWT secret is not configured' });
    }

    const user = await User.findByPk(req.user.userId);
    if (!user) {
      return res.status(404).json({ error: 'User not found' });
    }

    const remember = req.body?.remember !== false;
    const token = signToken(user, remember);
    res.json({ user: sanitizeUser(user), token, remember });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
