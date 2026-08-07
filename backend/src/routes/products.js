import express from 'express';
import { sequelize, Product, ProductVariant, Inventory, Category, Brand, Unit, User, SubCategory, Supplier } from '../models/index.js';
import ExpressError from '../utils/ExpressError.js';
import { authenticateToken } from '../middleware/auth.js';
import isAdmin from '../middleware/admin.js';

const router = express.Router();

// ─── Shared include for a fully-populated product ────────────────────────────
const productIncludes = [
  { model: Category, as: 'Category', attributes: ['name'] },
  { model: Brand, as: 'Brand', attributes: ['name'] },
  { model: Unit, as: 'Unit', attributes: ['name'] },
  { model: User, as: 'creator', attributes: ['name'] },
  {
    model: ProductVariant,
    as: 'ProductVariants',
    include: [{ model: Inventory, as: 'Inventories' }],
  },
];

const transformProduct = (p) => {
  const obj = typeof p.toJSON === 'function' ? p.toJSON() : p;
  const firstVariant = obj.ProductVariants?.[0] ?? null;
  const totalQty = (obj.ProductVariants || []).reduce((sum, v) => {
    return sum + (v.Inventories || []).reduce((s, i) => s + Number(i.qty || 0), 0);
  }, 0);

  return {
    ...obj,
    category: obj.Category?.name || '',
    categoryId: obj.category_id,
    brand: obj.Brand?.name || '',
    brandId: obj.brand_id,
    unit: obj.Unit?.name || '',
    unitId: obj.unit_id,
    subCategoryId: obj.sub_category_id,
    supplierId: obj.supplier_id,
    createdBy: obj.creator?.name || 'Unknown',
    createdById: obj.created_by,
    price: firstVariant ? Number(firstVariant.price || 0) : 0,
    qty: totalQty,
    // clean up nested objects already mapped above
    Category: undefined,
    Brand: undefined,
    Unit: undefined,
    creator: undefined,
  };
};

// GET /products
router.get('/', async (req, res) => {
  try {
    const products = await Product.findAll({ include: productIncludes });
    res.json(products.map(transformProduct));
  } catch (error) {
    console.error('GET /products error:', error);
    res.status(500).json({ error: error.message });
  }
});

// GET /products/pos
router.get('/pos', async (req, res) => {
  try {
    const { storeId } = req.query;
    if (!storeId) {
      return res.status(400).json({ message: 'storeId query parameter is required.' });
    }

    const products = await Product.findAll({
      where: { status: 'Active' },
      include: [
        { model: Category, as: 'Category', attributes: ['name'] },
        { model: Brand, as: 'Brand', attributes: ['name'] },
        { model: Unit, as: 'Unit', attributes: ['name'] },
        {
          model: ProductVariant,
          as: 'ProductVariants',
          include: [
            {
              model: Inventory,
              as: 'Inventories',
              where: { store_id: storeId },
              required: false,
            },
          ],
        },
      ],
    });

    const result = products
      .map(transformProduct)
      .filter((p) => p.ProductVariants && p.ProductVariants.some((v) => v.Inventories?.length > 0));

    res.json(result);
  } catch (error) {
    console.error('GET /products/pos error:', error);
    res.status(500).json({ error: error.message });
  }
});

// POST /products
router.post('/', authenticateToken, isAdmin, async (req, res) => {
  const t = await sequelize.transaction();
  try {
    const {
      name, status, description, categoryId, subCategoryId, brandId, unitId,
      productType, taxType, tax, discountType, discountValue,
      warranties, barcodeSymbology, sellingType, image, supplierId, slug,
      sku, itemBarcode, price, cost, weight, quantity, quantityAlert, storeId,
      variants,
    } = req.body;

    if (!name || !name.trim()) throw new ExpressError('Product name is required.', 400);
    if (!productType || !['single', 'variable'].includes(productType))
      throw new ExpressError('Invalid product type.', 400);

    const toNull = (v) => (v === '' || v === undefined ? null : v);

    const product = await Product.create(
      {
        name: name.trim(),
        status: toNull(status) || 'Active',
        description: toNull(description),
        category_id: toNull(categoryId),
        sub_category_id: toNull(subCategoryId),
        brand_id: toNull(brandId),
        unit_id: toNull(unitId),
        productType,
        taxType: toNull(taxType),
        tax: toNull(tax),
        created_by: req.user.userId,
        discountType: toNull(discountType),
        discountValue: toNull(discountValue),
        warranties: toNull(warranties),
        barcodeSymbology: toNull(barcodeSymbology),
        sellingType: toNull(sellingType),
        image: toNull(image),
        supplier_id: toNull(supplierId),
        slug: toNull(slug),
      },
      { transaction: t }
    );

    if (productType === 'single') {
      if (!sku || !price || !quantity || !storeId)
        throw new Error('Missing required fields for single product: sku, price, quantity, storeId');

      const variant = await ProductVariant.create(
        {
          product_id: product.id,
          sku,
          itemBarcode: toNull(itemBarcode),
          price: parseFloat(price),
          cost: toNull(cost) || 0,
          weight: toNull(weight) || 0,
          created_by: req.user.userId,
        },
        { transaction: t }
      );

      await Inventory.create(
        { variant_id: variant.id, store_id: storeId, qty: parseInt(quantity), quantity_alert: toNull(quantityAlert) || 0 },
        { transaction: t }
      );
    } else {
      if (!variants || !Array.isArray(variants) || variants.length === 0)
        throw new Error('Variable product must have at least one variant.');

      for (const v of variants) {
        const variant = await ProductVariant.create(
          {
            product_id: product.id,
            sku: v.sku,
            itemBarcode: toNull(v.itemBarcode),
            price: parseFloat(v.price) || 0,
            cost: toNull(v.cost) || 0,
            weight: toNull(v.weight) || 0,
            attributes: v.attributes || {},
            created_by: req.user.userId,
          },
          { transaction: t }
        );

        if (v.inventories && Array.isArray(v.inventories)) {
          for (const inv of v.inventories) {
            await Inventory.create(
              {
                variant_id: variant.id,
                store_id: inv.storeId,
                qty: parseInt(inv.qty) || 0,
                quantity_alert: toNull(inv.quantityAlert) || 0,
              },
              { transaction: t }
            );
          }
        }
      }
    }

    await t.commit();
    res.status(201).json(product.toJSON());
  } catch (error) {
    await t.rollback();
    console.error('POST /api/products error:', error);
    res.status(error.status || 500).json({ error: error.message });
  }
});

// PUT /products/:id
router.put('/:id', authenticateToken, isAdmin, async (req, res) => {
  const t = await sequelize.transaction();
  try {
    const productId = req.params.id;
    const { userId } = req.user;

    const product = await Product.findByPk(productId, { transaction: t });
    if (!product) throw new Error('Product not found');

    if (product.isLocked && product.locked_by && product.locked_by !== parseInt(userId)) {
      await t.rollback();
      return res.status(409).json({ message: 'This product is locked by another user and cannot be edited.' });
    }

    const {
      name, status, description, categoryId, subCategoryId, brandId, unitId,
      productType, taxType, tax, discountType, discountValue,
      warranties, barcodeSymbology, sellingType, image, supplierId, slug,
      variants,
    } = req.body;

    if (!name || !name.trim()) throw new ExpressError('Product name is required.', 400);
    if (!productType || !['single', 'variable'].includes(productType))
      throw new ExpressError('Invalid product type.', 400);
    if (!Array.isArray(variants)) throw new ExpressError('Variants must be provided as an array.', 400);

    const toNull = (v) => (v === '' || v === undefined ? null : v);

    await product.update(
      {
        name: name.trim(),
        status: toNull(status) || product.status || 'Active',
        description: toNull(description),
        category_id: toNull(categoryId),
        sub_category_id: toNull(subCategoryId),
        brand_id: toNull(brandId),
        unit_id: toNull(unitId),
        productType,
        taxType: toNull(taxType),
        tax: toNull(tax),
        discountType: toNull(discountType),
        discountValue: toNull(discountValue),
        warranties: toNull(warranties),
        barcodeSymbology: toNull(barcodeSymbology),
        sellingType: toNull(sellingType),
        image: toNull(image),
        supplier_id: toNull(supplierId),
        slug: toNull(slug),
      },
      { transaction: t }
    );

    // Determine variants to delete
    const existingVariants = await ProductVariant.findAll({ where: { product_id: productId }, transaction: t });
    const existingIds = existingVariants.map((v) => v.id);
    const incomingIds = variants.map((v) => v.id || v._id).filter(Boolean).map(Number);
    const toDelete = existingIds.filter((id) => !incomingIds.includes(id));

    if (toDelete.length > 0) {
      await Inventory.destroy({ where: { variant_id: toDelete }, transaction: t });
      await ProductVariant.destroy({ where: { id: toDelete }, transaction: t });
    }

    for (const v of variants) {
      const variantData = {
        product_id: product.id,
        sku: v.sku,
        itemBarcode: toNull(v.itemBarcode),
        price: parseFloat(v.price) || 0,
        cost: toNull(v.cost) || 0,
        weight: toNull(v.weight) || 0,
        attributes: v.attributes || {},
      };

      let savedVariant;
      if (v.id || v._id) {
        savedVariant = await ProductVariant.findByPk(v.id || v._id, { transaction: t });
        if (savedVariant) await savedVariant.update(variantData, { transaction: t });
      } else {
        savedVariant = await ProductVariant.create({ ...variantData, created_by: userId }, { transaction: t });
      }

      if (savedVariant) {
        if (v.inventories && Array.isArray(v.inventories)) {
          await Inventory.destroy({ where: { variant_id: savedVariant.id }, transaction: t });
          for (const inv of v.inventories) {
            await Inventory.create(
              { variant_id: savedVariant.id, store_id: inv.storeId, qty: parseInt(inv.qty) || 0, quantity_alert: toNull(inv.quantityAlert) || 0 },
              { transaction: t }
            );
          }
        } else if (productType === 'single' && v.quantity !== undefined && v.storeId) {
          await Inventory.destroy({ where: { variant_id: savedVariant.id }, transaction: t });
          await Inventory.create(
            { variant_id: savedVariant.id, store_id: v.storeId, qty: parseInt(v.quantity) || 0, quantity_alert: toNull(v.quantityAlert) || 0 },
            { transaction: t }
          );
        }
      }
    }

    await t.commit();
    res.status(200).json(product.toJSON());
  } catch (error) {
    await t.rollback();
    console.error('PUT /api/products/:id error:', error);
    res.status(error.status || 500).json({ error: error.message });
  }
});

// DELETE /products/:id
router.delete('/:id', authenticateToken, isAdmin, async (req, res) => {
  const t = await sequelize.transaction();
  try {
    const product = await Product.findByPk(req.params.id, { transaction: t });
    if (!product) throw new ExpressError('Product not found', 404);

    const variants = await ProductVariant.findAll({ where: { product_id: product.id }, transaction: t });
    if (variants.length > 0) {
      const variantIds = variants.map((v) => v.id);
      await Inventory.destroy({ where: { variant_id: variantIds }, transaction: t });
      await ProductVariant.destroy({ where: { product_id: product.id }, transaction: t });
    }

    await product.destroy({ transaction: t });
    await t.commit();
    res.status(204).send();
  } catch (error) {
    await t.rollback();
    console.error('DELETE /products/:id error:', error);
    res.status(error.status || 500).json({ error: error.message });
  }
});

// PUT /products/:id/lock
router.put('/:id/lock', authenticateToken, async (req, res) => {
  try {
    const { lock, storeId } = req.body;
    const { userId } = req.user;
    const product = await Product.findByPk(req.params.id);
    if (!product) return res.status(404).json({ message: 'Product not found.' });

    if (lock) {
      const twoMinutesAgo = new Date(Date.now() - 2 * 60 * 1000);
      // Can lock if: not locked, lock expired, or same user
      if (
        product.isLocked &&
        product.locked_by !== parseInt(userId) &&
        product.lockedAt > twoMinutesAgo
      ) {
        return res.status(409).json({ message: 'Product is currently locked by another user.' });
      }
      await product.update({ isLocked: true, lockedAt: new Date(), locked_by: userId });
    } else {
      if (product.locked_by !== parseInt(userId)) {
        return res.status(403).json({ message: 'You cannot unlock a product locked by another user.' });
      }
      await product.update({ isLocked: false, lockedAt: null, locked_by: null });
    }

    // Return full product details
    const full = await Product.findByPk(product.id, { include: productIncludes });
    res.json(transformProduct(full));
  } catch (error) {
    console.error('PUT /products/:id/lock error:', error);
    res.status(500).json({ message: 'Server error while updating product lock.', error: error.message });
  }
});

export default router;
