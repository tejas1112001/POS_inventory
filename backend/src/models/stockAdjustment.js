import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class StockAdjustment extends Model {}

StockAdjustment.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    type: { type: DataTypes.ENUM('Addition', 'Subtraction'), allowNull: false },
    quantity: { type: DataTypes.INTEGER, allowNull: false },
    reason: { type: DataTypes.STRING, defaultValue: 'Manual Correction' },
    note: { type: DataTypes.TEXT },
  },
  {
    sequelize,
    modelName: 'StockAdjustment',
    tableName: 'stock_adjustments',
    timestamps: true,
    indexes: [
      { fields: ['variant_id'] },
      { fields: ['store_id'] },
      { fields: ['created_by'] },
    ],
  }
);

export default StockAdjustment;
