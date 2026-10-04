import { DataTypes, Model } from 'sequelize';
import sequelize from '../config/db.js';

class Store extends Model {}

Store.init(
  {
    id: { type: DataTypes.INTEGER, autoIncrement: true, primaryKey: true },
    name: { type: DataTypes.STRING, allowNull: false },
    slug: { type: DataTypes.STRING, unique: true },
    address: { type: DataTypes.STRING },
    status: { type: DataTypes.ENUM('Active', 'Inactive'), defaultValue: 'Active' },
    logo: { type: DataTypes.TEXT },
    banner: { type: DataTypes.TEXT },
    description: { type: DataTypes.TEXT },
    phone: { type: DataTypes.STRING },
    openingHours: { type: DataTypes.STRING, field: 'opening_hours' },
    isPublicEnabled: { type: DataTypes.BOOLEAN, defaultValue: true, field: 'is_public_enabled' },
  },
  {
    sequelize,
    modelName: 'Store',
    tableName: 'stores',
    timestamps: true,
    indexes: [
      { fields: ['slug'], unique: true },
      { fields: ['status'] },
    ],
  }
);

export default Store;
