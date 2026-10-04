import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class Role extends Model {}

Role.init(
  {
    id:          { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    name:        { type: DataTypes.STRING(60), allowNull: false, unique: true },
    description: { type: DataTypes.STRING(255), allowNull: true },
    color:       { type: DataTypes.STRING(20), defaultValue: '#6366f1' },
    is_system:   { type: DataTypes.BOOLEAN, defaultValue: false, allowNull: false },
  },
  {
    sequelize,
    modelName: 'Role',
    tableName: 'roles',
    timestamps: true,
  }
);

export default Role;
