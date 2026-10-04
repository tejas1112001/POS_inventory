import express from 'express';
import bcrypt from 'bcrypt';
import jwt from 'jsonwebtoken';
import { User } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import { getPermissionsForRole } from '../services/permissionService.js';

const router = express.Router();

const REMEMBER_EXPIRY = '30d';
const SESSION_EXPIRY = '12h';

// Biller legacy permissions (fallback when role_id is null)
const LEGACY_BILLER_PERMS = [
  'dashboard:view', 'products:view',
  'pos:view', 'pos:create',
  'sales:view', 'sales:create',
  'customers:view', 'customers:create',
  'inventory:view', 'reports:view', 'invoices:view', 'settings:view',
];

function signToken(user, remember = true) {
  const secret = process.env.JWT_SECRET;
  return jwt.sign(
    { userId: user.id, role: user.role, roleId: user.role_id || null },
    secret,
    { expiresIn: remember ? REMEMBER_EXPIRY : SESSION_EXPIRY }
  );
}

function sanitizeUser(user) {
  const userJson = typeof user.toJSON === 'function' ? user.toJSON() : { ...user };
  delete userJson.password;
  return userJson;
}

async function buildPermissions(user) {
  if (user.role_id) {
    const perms = await getPermissionsForRole(user.role_id);
    return [...perms];
  }
  // Legacy: role_id not yet assigned
  if (user.role === 'admin') return ['*']; // wildcard — all access
  return LEGACY_BILLER_PERMS;
}

router.post('/login', async (req, res) => {
  try {
    const { email, password, remember = true } = req.body;
    if (!email || !password) {
      return res.status(400).json({ error: 'Email and password are required' });
    }

    const secret = process.env.JWT_SECRET;
    if (!secret) return res.status(500).json({ error: 'JWT secret is not configured' });

    const user = await User.findOne({ where: { email } });
    if (!user) return res.status(404).json({ error: 'User not found' });

    const validPassword = await bcrypt.compare(password, user.password);
    if (!validPassword) return res.status(401).json({ error: 'Invalid password' });

    const token = signToken(user, remember);
    const permissions = await buildPermissions(user);

    res.json({ user: sanitizeUser(user), token, remember, permissions });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.post('/signup', async (req, res) => {
  try {
    const { email, password, role_id, remember = true, ...rest } = req.body;
    if (!email || !password) {
      return res.status(400).json({ error: 'Email and password are required' });
    }

    const secret = process.env.JWT_SECRET;
    if (!secret) return res.status(500).json({ error: 'JWT secret is not configured' });

    const hashedPassword = await bcrypt.hash(password, 10);
    let initialRole = rest.role || 'biller';
    let targetRoleId = role_id ? parseInt(role_id) : null;

    if (targetRoleId) {
      const { Role } = await import('../models/index.js');
      const targetRole = await Role.findByPk(targetRoleId);
      if (targetRole) {
        initialRole = targetRole.name.toLowerCase() === 'admin' ? 'admin' : 'biller';
      }
    }

    const user = await User.create({
      ...rest,
      email,
      password: hashedPassword,
      role: initialRole,
      role_id: targetRoleId,
    });

    if (!user.role_id) {
      try {
        const { Role } = await import('../models/index.js');
        const defaultRole = await Role.findOne({ where: { name: initialRole.toLowerCase() } });
        if (defaultRole) await user.update({ role_id: defaultRole.id });
      } catch { /* non-fatal */ }
    }

    const token = signToken(user, remember);
    const permissions = await buildPermissions(user);
    res.status(201).json({ user: sanitizeUser(user), token, remember, permissions });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

router.post('/refresh', authenticateToken, async (req, res) => {
  try {
    const secret = process.env.JWT_SECRET;
    if (!secret) return res.status(500).json({ error: 'JWT secret is not configured' });

    const user = await User.findByPk(req.user.userId);
    if (!user) return res.status(404).json({ error: 'User not found' });

    const remember = req.body?.remember !== false;
    const token = signToken(user, remember);
    const permissions = await buildPermissions(user);

    res.json({ user: sanitizeUser(user), token, remember, permissions });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// GET /api/auth/permissions — real-time permission refresh (no new token issued)
router.get('/permissions', authenticateToken, async (req, res) => {
  try {
    const user = await User.findByPk(req.user.userId);
    if (!user) return res.status(404).json({ error: 'User not found' });

    const permissions = await buildPermissions(user);
    res.json({ permissions });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
