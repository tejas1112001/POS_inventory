import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class PurchaseItem extends Model {}

PurchaseItem.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    quantity: { type: DataTypes.INTEGER, allowNull: false },
    unitPrice: { type: DataTypes.FLOAT, allowNull: false, field: 'unit_price' },
    discount: { type: DataTypes.FLOAT, defaultValue: 0 },
    subtotal: { type: DataTypes.FLOAT, allowNull: false },
  },
  {
    sequelize,
    modelName: 'PurchaseItem',
    tableName: 'purchase_items',
    timestamps: true,
  }
);

export default PurchaseItem;
