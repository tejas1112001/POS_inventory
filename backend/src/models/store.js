import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class Store extends Model {}

Store.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    name: { type: DataTypes.STRING, allowNull: false },
    address: { type: DataTypes.STRING },
    status: { type: DataTypes.ENUM('Active', 'Inactive'), defaultValue: 'Active' },
  },
  {
    sequelize,
    modelName: 'Store',
    tableName: 'stores',
    timestamps: true,
  }
);

export default Store;
