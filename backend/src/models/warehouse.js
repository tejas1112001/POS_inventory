import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class Warehouse extends Model {}

Warehouse.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    name: { type: DataTypes.STRING, allowNull: false },
    address: { type: DataTypes.STRING },
    status: { type: DataTypes.ENUM('Active', 'Inactive'), defaultValue: 'Active' },
  },
  {
    sequelize,
    modelName: 'Warehouse',
    tableName: 'warehouses',
    timestamps: true,
  }
);

export default Warehouse;
