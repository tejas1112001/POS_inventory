// middleware/auth.js
import jwt from 'jsonwebtoken';
import { User } from '../models/index.js';

export const authenticateToken = async (req, res, next) => {
  try {
    const authHeader = req.headers['authorization'];
    const token = authHeader && authHeader.split(' ')[1];

    if (!token) {
      return res.status(401).json({ error: 'Access token required' });
    }

    const secret = process.env.JWT_SECRET;
    if (!secret) {
      return res.status(500).json({ error: 'JWT secret is not configured' });
    }

    // Synchronous verify — throws on invalid/expired token
    let payload;
    try {
      payload = jwt.verify(token, secret);
    } catch (err) {
      return res.status(403).json({ error: 'Invalid or expired token' });
    }

    let roleId = payload.roleId ?? null;

    // Migration path: old tokens without roleId — do a single DB lookup
    if (!roleId) {
      try {
        const user = await User.findByPk(payload.userId, { attributes: ['role_id'] });
        roleId = user?.role_id ?? null;
      } catch {
        // Non-fatal — roleId stays null, requirePermission will deny accordingly
      }
    }

    req.user = { userId: payload.userId, role: payload.role, roleId };
    next();
  } catch (err) {
    next(err);
  }
};
