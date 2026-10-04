import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class ProductVariant extends Model {}

ProductVariant.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    sku: { type: DataTypes.STRING },
    itemBarcode: { type: DataTypes.STRING, field: 'item_barcode' },
    price: { type: DataTypes.FLOAT },
    cost: { type: DataTypes.FLOAT, defaultValue: 0 },
    weight: { type: DataTypes.FLOAT, defaultValue: 0 },
    // JSONB stores arbitrary key/value attributes for variable products
    attributes: { type: DataTypes.JSONB, defaultValue: {} },
    expiryDate: { type: DataTypes.DATE, field: 'expiry_date' },
    manufacturedDate: { type: DataTypes.DATE, field: 'manufactured_date' },
  },
  {
    sequelize,
    modelName: 'ProductVariant',
    tableName: 'product_variants',
    timestamps: true,
    indexes: [
      { fields: ['sku'] },
      { fields: ['item_barcode'] },
      { fields: ['product_id'] },
    ],
  }
);

export default ProductVariant;
