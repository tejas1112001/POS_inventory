import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class StoreProduct extends Model {}

StoreProduct.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    store_id: { type: DataTypes.INTEGER, allowNull: false },
    product_id: { type: DataTypes.INTEGER, allowNull: false },
    isPublished: { type: DataTypes.BOOLEAN, defaultValue: false, field: 'is_published' },
    isFeatured: { type: DataTypes.BOOLEAN, defaultValue: false, field: 'is_featured' },
  },
  {
    sequelize,
    modelName: 'StoreProduct',
    tableName: 'store_products',
    timestamps: true,
    indexes: [
      { fields: ['store_id', 'product_id'], unique: true },
      { fields: ['store_id', 'is_published'] },
      { fields: ['store_id', 'is_featured'] },
    ],
  }
);

export default StoreProduct;
