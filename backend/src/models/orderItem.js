import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class OrderItem extends Model {}

OrderItem.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    quantity: { type: DataTypes.INTEGER, allowNull: false },
    unit_price: { type: DataTypes.FLOAT, allowNull: false },
    discount: { type: DataTypes.FLOAT, defaultValue: 0 },
    subtotal: { type: DataTypes.FLOAT, allowNull: false },
  },
  {
    sequelize,
    modelName: 'OrderItem',
    tableName: 'order_items',
    timestamps: true,
    indexes: [
      { fields: ['sale_id'] },
      { fields: ['product_id'] },
      { fields: ['variant_id'] },
    ],
  }
);

export default OrderItem;
