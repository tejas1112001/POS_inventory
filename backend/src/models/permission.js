import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class Permission extends Model {}

Permission.init(
  {
    id:          { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    module:      { type: DataTypes.STRING(60), allowNull: false },
    action:      { type: DataTypes.STRING(30), allowNull: false }, // view|create|edit|delete|approve
    label:       { type: DataTypes.STRING(120), allowNull: true },
  },
  {
    sequelize,
    modelName: 'Permission',
    tableName: 'permissions',
    timestamps: false,
    indexes: [{ unique: true, fields: ['module', 'action'] }],
  }
);

export default Permission;
