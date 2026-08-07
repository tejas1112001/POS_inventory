import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class SubCategory extends Model {}

SubCategory.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    name: { type: DataTypes.STRING, allowNull: false },
    status: { type: DataTypes.ENUM('Active', 'Inactive'), defaultValue: 'Active' },
  },
  {
    sequelize,
    modelName: 'SubCategory',
    tableName: 'sub_categories',
    timestamps: true,
  }
);

export default SubCategory;
