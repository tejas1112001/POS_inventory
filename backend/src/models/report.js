import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class Report extends Model {}

Report.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    type: { type: DataTypes.STRING },
    reference: { type: DataTypes.STRING },
    date: { type: DataTypes.DATE },
    amount: { type: DataTypes.FLOAT },
    payment_method: { type: DataTypes.STRING },
    payment_status: { type: DataTypes.STRING },
    customer_name: { type: DataTypes.STRING },
    customer_image: { type: DataTypes.TEXT },
    product_name: { type: DataTypes.STRING },
    product_image: { type: DataTypes.TEXT },
    category: { type: DataTypes.STRING },
  },
  {
    sequelize,
    modelName: 'Report',
    tableName: 'reports',
    timestamps: true,
  }
);

export default Report;
