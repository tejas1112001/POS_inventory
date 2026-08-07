import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class SaleReturn extends Model {}

SaleReturn.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    reference: { type: DataTypes.STRING },
    date: { type: DataTypes.DATE },
    status: {
      type: DataTypes.ENUM('Pending', 'Completed'),
      defaultValue: 'Pending',
    },
    total: { type: DataTypes.FLOAT, allowNull: false },
    paid: { type: DataTypes.FLOAT, defaultValue: 0 },
    due: { type: DataTypes.FLOAT },
    payment_status: {
      type: DataTypes.ENUM('Paid', 'Unpaid', 'Partial'),
      defaultValue: 'Unpaid',
    },
  },
  {
    sequelize,
    modelName: 'SaleReturn',
    tableName: 'sale_returns',
    timestamps: true,
  }
);

export default SaleReturn;
