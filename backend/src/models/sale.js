import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class Sale extends Model {}

Sale.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    reference: { type: DataTypes.STRING },
    date: { type: DataTypes.DATE },
    status: { type: DataTypes.ENUM('Completed', 'Pending') },
    payment_status: { type: DataTypes.ENUM('Paid', 'Unpaid', 'Overdue') },
    payment_method: { type: DataTypes.STRING },
    subtotal: { type: DataTypes.FLOAT },
    discount: { type: DataTypes.FLOAT },
    tax: { type: DataTypes.FLOAT },
    shipping: { type: DataTypes.FLOAT },
    total: { type: DataTypes.FLOAT },
    paid: { type: DataTypes.FLOAT },
    due: { type: DataTypes.FLOAT },
    note: { type: DataTypes.TEXT },
  },
  {
    sequelize,
    modelName: 'Sale',
    tableName: 'sales',
    timestamps: true,
    indexes: [
      { fields: ['reference'] },
      { fields: ['date'] },
      { fields: ['customer_id'] },
      { fields: ['store_id'] },
      { fields: ['user_id'] },
    ],
  }
);

export default Sale;
