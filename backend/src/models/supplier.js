import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class Supplier extends Model {}

Supplier.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    name: { type: DataTypes.STRING, allowNull: false },
    email: { type: DataTypes.STRING },
    phone: { type: DataTypes.STRING },
    address: { type: DataTypes.STRING },
    image: { type: DataTypes.TEXT },
    status: { type: DataTypes.ENUM('Active', 'Inactive'), defaultValue: 'Active' },
  },
  {
    sequelize,
    modelName: 'Supplier',
    tableName: 'suppliers',
    timestamps: true,
  }
);

export default Supplier;
