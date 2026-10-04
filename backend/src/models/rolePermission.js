import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class RolePermission extends Model {}

RolePermission.init(
  {
    role_id:       { type: DataTypes.INTEGER, primaryKey: true, allowNull: false },
    permission_id: { type: DataTypes.INTEGER, primaryKey: true, allowNull: false },
  },
  {
    sequelize,
    modelName: 'RolePermission',
    tableName: 'role_permissions',
    timestamps: false,
  }
);

export default RolePermission;
