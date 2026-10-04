import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class Purchase extends Model {}

Purchase.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    reference: { type: DataTypes.STRING },
    date: { type: DataTypes.DATE },
    status: { type: DataTypes.STRING },
    payment_status: { type: DataTypes.ENUM('Paid', 'Unpaid', 'Overdue') },
    total: { type: DataTypes.FLOAT },
    paid: { type: DataTypes.FLOAT },
    due: { type: DataTypes.FLOAT },
  },
  {
    sequelize,
    modelName: 'Purchase',
    tableName: 'purchases',
    timestamps: true,
    indexes: [
      { fields: ['reference'] },
      { fields: ['status'] },
      { fields: ['date'] },
      { fields: ['supplier_id'] },
    ],
  }
);

export default Purchase;
