import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class Brand extends Model {}

Brand.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    name: { type: DataTypes.STRING, allowNull: false },
    status: { type: DataTypes.ENUM('Active', 'Inactive'), defaultValue: 'Active' },
    image: { type: DataTypes.TEXT },
  },
  {
    sequelize,
    modelName: 'Brand',
    tableName: 'brands',
    timestamps: true,
  }
);

export default Brand;
