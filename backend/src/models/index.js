import sequelize from '../config/db.js';
import Brand from './brand.js';
import Category from './category.js';
import Customer from './customer.js';
import Inventory from './inventory.js';
import OrderItem from './orderItem.js';
import Product from './product.js';
import ProductVariant from './productVariant.js';
import Purchase from './purchase.js';
import PurchaseItem from './purchaseItem.js';
import Report from './report.js';
import ReturnItem from './returnItems.js';
import Sale from './sale.js';
import SaleReturn from './saleReturn.js';
import SubCategory from './subCategory.js';
import Supplier from './supplier.js';
import Unit from './unit.js';
import User from './user.js';
import Store from './store.js';
import Warehouse from './warehouse.js';

// ─── User associations ────────────────────────────────────────────────────────
Brand.belongsTo(User, { foreignKey: 'created_by', as: 'creator' });
Category.belongsTo(User, { foreignKey: 'created_by', as: 'creator' });
Customer.belongsTo(User, { foreignKey: 'created_by', as: 'creator' });
Supplier.belongsTo(User, { foreignKey: 'created_by', as: 'creator' });
Unit.belongsTo(User, { foreignKey: 'created_by', as: 'creator' });
Store.belongsTo(User, { foreignKey: 'created_by', as: 'creator' });
Warehouse.belongsTo(User, { foreignKey: 'created_by', as: 'creator' });
SubCategory.belongsTo(User, { foreignKey: 'created_by', as: 'creator' });
Product.belongsTo(User, { foreignKey: 'created_by', as: 'creator' });
Product.belongsTo(User, { foreignKey: 'locked_by', as: 'locker' });
ProductVariant.belongsTo(User, { foreignKey: 'created_by', as: 'creator' });
Purchase.belongsTo(User, { foreignKey: 'created_by', as: 'creator' });
SaleReturn.belongsTo(User, { foreignKey: 'created_by', as: 'creator' });

// ─── Product associations ─────────────────────────────────────────────────────
Product.belongsTo(Category, { foreignKey: 'category_id', as: 'Category' });
Product.belongsTo(SubCategory, { foreignKey: 'sub_category_id', as: 'SubCategory' });
Product.belongsTo(Brand, { foreignKey: 'brand_id', as: 'Brand' });
Product.belongsTo(Unit, { foreignKey: 'unit_id', as: 'Unit' });
Product.belongsTo(Supplier, { foreignKey: 'supplier_id', as: 'Supplier' });

Category.hasMany(Product, { foreignKey: 'category_id' });
SubCategory.belongsTo(Category, { foreignKey: 'category_id', as: 'Category' });
Category.hasMany(SubCategory, { foreignKey: 'category_id' });

// ─── ProductVariant associations ──────────────────────────────────────────────
ProductVariant.belongsTo(Product, { foreignKey: 'product_id', as: 'Product' });
Product.hasMany(ProductVariant, { foreignKey: 'product_id', as: 'ProductVariants' });

// ─── Inventory associations ───────────────────────────────────────────────────
Inventory.belongsTo(ProductVariant, { foreignKey: 'variant_id', as: 'Variant' });
Inventory.belongsTo(Store, { foreignKey: 'store_id', as: 'Store' });
ProductVariant.hasMany(Inventory, { foreignKey: 'variant_id', as: 'Inventories' });

// ─── Sale associations ────────────────────────────────────────────────────────
Sale.belongsTo(Customer, { foreignKey: 'customer_id', as: 'customer' });
Sale.belongsTo(User, { foreignKey: 'user_id', as: 'user' });
Sale.belongsTo(Store, { foreignKey: 'store_id', as: 'Store' });
Sale.hasMany(OrderItem, { foreignKey: 'sale_id', as: 'items' });

// ─── OrderItem associations ───────────────────────────────────────────────────
OrderItem.belongsTo(Sale, { foreignKey: 'sale_id', as: 'sale' });
OrderItem.belongsTo(Product, { foreignKey: 'product_id', as: 'product' });
OrderItem.belongsTo(ProductVariant, { foreignKey: 'variant_id', as: 'variant' });

// ─── Purchase associations ────────────────────────────────────────────────────
Purchase.belongsTo(Supplier, { foreignKey: 'supplier_id', as: 'supplier' });
Purchase.hasMany(PurchaseItem, { foreignKey: 'purchase_id', as: 'items' });

// ─── PurchaseItem associations ────────────────────────────────────────────────
PurchaseItem.belongsTo(Purchase, { foreignKey: 'purchase_id', as: 'Purchase' });
PurchaseItem.belongsTo(Product, { foreignKey: 'product_id', as: 'product' });

// ─── SaleReturn associations ──────────────────────────────────────────────────
SaleReturn.belongsTo(Customer, { foreignKey: 'customer_id', as: 'customer' });
SaleReturn.belongsTo(Sale, { foreignKey: 'sale_id', as: 'Sale' });
SaleReturn.belongsTo(Store, { foreignKey: 'store_id', as: 'Store' });
SaleReturn.hasMany(ReturnItem, { foreignKey: 'sale_return_id', as: 'items' });

// ─── ReturnItem associations ──────────────────────────────────────────────────
ReturnItem.belongsTo(SaleReturn, { foreignKey: 'sale_return_id', as: 'SaleReturn' });
ReturnItem.belongsTo(Product, { foreignKey: 'product_id', as: 'product' });
ReturnItem.belongsTo(ProductVariant, { foreignKey: 'variant_id', as: 'variant' });

export {
  sequelize,
  Brand,
  Category,
  Customer,
  Inventory,
  OrderItem,
  Product,
  ProductVariant,
  Purchase,
  PurchaseItem,
  Report,
  ReturnItem,
  Sale,
  SaleReturn,
  SubCategory,
  Supplier,
  Unit,
  User,
  Store,
  Warehouse,
};
