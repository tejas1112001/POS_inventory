import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class Product extends Model {}

Product.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    name: { type: DataTypes.STRING, allowNull: false },
    description: { type: DataTypes.TEXT },
    status: { type: DataTypes.ENUM('Active', 'Inactive'), defaultValue: 'Active' },
    image: { type: DataTypes.TEXT },
    slug: { type: DataTypes.STRING },
    sellingType: { type: DataTypes.ENUM('Online', 'POS'), field: 'selling_type' },
    productType: {
      type: DataTypes.ENUM('single', 'variable'),
      allowNull: false,
      field: 'product_type',
    },
    taxType: { type: DataTypes.ENUM('Exclusive', 'Inclusive'), field: 'tax_type' },
    tax: { type: DataTypes.FLOAT },
    discountType: { type: DataTypes.ENUM('Percentage', 'Fixed'), field: 'discount_type' },
    discountValue: { type: DataTypes.FLOAT, field: 'discount_value' },
    warranties: { type: DataTypes.STRING },
    barcodeSymbology: { type: DataTypes.STRING, field: 'barcode_symbology' },
    isLocked: { type: DataTypes.BOOLEAN, defaultValue: false, field: 'is_locked' },
    lockedAt: { type: DataTypes.DATE, field: 'locked_at' },
    isPublished: { type: DataTypes.BOOLEAN, defaultValue: false, field: 'is_published' },
    isFeatured: { type: DataTypes.BOOLEAN, defaultValue: false, field: 'is_featured' },
  },
  {
    sequelize,
    modelName: 'Product',
    tableName: 'products',
    timestamps: true,
    indexes: [
      { fields: ['name'] },
      { fields: ['status'] },
      { fields: ['is_published'] },
      { fields: ['is_featured'] },
      { fields: ['category_id'] },
      { fields: ['brand_id'] },
      { fields: ['unit_id'] },
    ],
  }
);

export default Product;
