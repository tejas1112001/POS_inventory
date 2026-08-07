import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class ReturnItem extends Model {}

ReturnItem.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    quantity: { type: DataTypes.INTEGER, allowNull: false },
    unitPrice: { type: DataTypes.FLOAT, allowNull: false, field: 'unit_price' },
    reason: { type: DataTypes.STRING },
    subtotal: { type: DataTypes.FLOAT, allowNull: false },
  },
  {
    sequelize,
    modelName: 'ReturnItem',
    tableName: 'return_items',
    timestamps: true,
  }
);

export default ReturnItem;
