import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class Inventory extends Model {}

Inventory.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    qty: { type: DataTypes.INTEGER, defaultValue: 0 },
    quantityAlert: { type: DataTypes.INTEGER, defaultValue: 0, field: 'quantity_alert' },
  },
  {
    sequelize,
    modelName: 'Inventory',
    tableName: 'inventories',
    timestamps: true,
    indexes: [
      { fields: ['variant_id', 'store_id'], unique: true },
      { fields: ['store_id'] },
    ],
  }
);

export default Inventory;
