import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class Unit extends Model {}

Unit.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    name: { type: DataTypes.STRING, allowNull: false },
    short_name: { type: DataTypes.STRING },
    status: { type: DataTypes.ENUM('Active', 'Inactive'), defaultValue: 'Active' },
  },
  {
    sequelize,
    modelName: 'Unit',
    tableName: 'units',
    timestamps: true,
  }
);

export default Unit;
