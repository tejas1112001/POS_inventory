import express from 'express';
import { Role, Permission, RolePermission, User, sequelize } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import { requirePermission } from '../middleware/permission.js';
import { invalidateRoleCache } from '../services/permissionService.js';

const router = express.Router();

// GET /api/roles — list all roles with permissions and user count
router.get('/', authenticateToken, requirePermission('roles', 'view'), async (req, res) => {
  try {
    const roles = await Role.findAll({
      include: [
        {
          model: Permission,
          as: 'permissions',
          through: { attributes: [] },
        },
        {
          model: User,
          as: 'users',
          attributes: ['id'],
        },
      ],
      order: [['id', 'ASC']],
    });

    const formatted = roles.map((role) => {
      const json = role.toJSON();
      return {
        id: json.id,
        name: json.name,
        description: json.description,
        is_system: json.is_system,
        createdAt: json.createdAt,
        userCount: json.users ? json.users.length : 0,
        permissions: (json.permissions || []).map((p) => `${p.module}:${p.action}`),
        permissionIds: (json.permissions || []).map((p) => p.id),
      };
    });

    res.json(formatted);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// GET /api/roles/permissions — list all available system permissions
router.get('/permissions', authenticateToken, requirePermission('roles', 'view'), async (req, res) => {
  try {
    const permissions = await Permission.findAll({
      order: [
        ['module', 'ASC'],
        ['action', 'ASC'],
      ],
    });
    res.json(permissions);
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// POST /api/roles — create new custom role
router.post('/', authenticateToken, requirePermission('roles', 'create'), async (req, res) => {
  const t = await sequelize.transaction();
  try {
    const { name, description, permissionIds } = req.body;
    if (!name || !name.trim()) {
      return res.status(400).json({ error: 'Role name is required.' });
    }

    const existing = await Role.findOne({ where: { name: name.trim() } });
    if (existing) {
      return res.status(409).json({ error: 'Role with this name already exists.' });
    }

    const role = await Role.create(
      { name: name.trim(), description: description || '', is_system: false },
      { transaction: t }
    );

    if (Array.isArray(permissionIds) && permissionIds.length > 0) {
      const links = permissionIds.map((pId) => ({
        role_id: role.id,
        permission_id: pId,
      }));
      await RolePermission.bulkCreate(links, { transaction: t });
    }

    await t.commit();
    invalidateRoleCache(role.id);

    const created = await Role.findByPk(role.id, {
      include: [{ model: Permission, as: 'permissions', through: { attributes: [] } }],
    });
    res.status(201).json(created);
  } catch (error) {
    await t.rollback();
    res.status(500).json({ error: error.message });
  }
});

// PUT /api/roles/:id — update role & its assigned permissions
router.put('/:id', authenticateToken, requirePermission('roles', 'edit'), async (req, res) => {
  const t = await sequelize.transaction();
  try {
    const roleId = req.params.id;
    const role = await Role.findByPk(roleId);
    if (!role) return res.status(404).json({ error: 'Role not found.' });

    const { name, description, permissionIds } = req.body;

    if (name && name.trim()) {
      role.name = name.trim();
    }
    if (description !== undefined) {
      role.description = description;
    }
    await role.save({ transaction: t });

    if (Array.isArray(permissionIds)) {
      await RolePermission.destroy({ where: { role_id: roleId }, transaction: t });
      if (permissionIds.length > 0) {
        const links = permissionIds.map((pId) => ({
          role_id: roleId,
          permission_id: pId,
        }));
        await RolePermission.bulkCreate(links, { transaction: t });
      }
    }

    await t.commit();
    invalidateRoleCache(roleId);

    const updated = await Role.findByPk(roleId, {
      include: [{ model: Permission, as: 'permissions', through: { attributes: [] } }],
    });
    res.json(updated);
  } catch (error) {
    await t.rollback();
    res.status(500).json({ error: error.message });
  }
});

// DELETE /api/roles/:id — delete custom role
router.delete('/:id', authenticateToken, requirePermission('roles', 'delete'), async (req, res) => {
  try {
    const role = await Role.findByPk(req.params.id);
    if (!role) return res.status(404).json({ error: 'Role not found.' });

    if (role.is_system) {
      return res.status(400).json({ error: 'System roles cannot be deleted.' });
    }

    const userCount = await User.count({ where: { role_id: role.id } });
    if (userCount > 0) {
      return res.status(400).json({ error: `Cannot delete role assigned to ${userCount} user(s). Reassign them first.` });
    }

    await RolePermission.destroy({ where: { role_id: role.id } });
    await role.destroy();

    invalidateRoleCache(role.id);
    res.status(204).send();
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

// POST /api/roles/assign-user — assign role to a user
router.post('/assign-user', authenticateToken, requirePermission('users', 'edit'), async (req, res) => {
  try {
    const { userId, roleId } = req.body;
    if (!userId || !roleId) {
      return res.status(400).json({ error: 'userId and roleId are required.' });
    }

    const user = await User.findByPk(userId);
    if (!user) return res.status(404).json({ error: 'User not found.' });

    const role = await Role.findByPk(roleId);
    if (!role) return res.status(404).json({ error: 'Role not found.' });

    user.role_id = role.id;
    user.role = role.name.toLowerCase() === 'admin' ? 'admin' : 'biller';
    await user.save();

    invalidateRoleCache(user.role_id);

    res.json({ message: 'Role assigned successfully', user: { id: user.id, name: user.name, role_id: user.role_id, role: user.role } });
  } catch (error) {
    res.status(500).json({ error: error.message });
  }
});

export default router;
