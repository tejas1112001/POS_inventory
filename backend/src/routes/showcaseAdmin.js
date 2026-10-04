import express from 'express';
import { Store, Product, StoreProduct, Category, Brand, ProductVariant } from '../models/index.js';
import { authenticateToken } from '../middleware/auth.js';
import { requirePermission } from '../middleware/permission.js';

const router = express.Router({ mergeParams: true });

// ─── GET /api/stores/:id/showcase ─── Get store showcase profile & product publishing list
router.get('/:id/showcase', authenticateToken, async (req, res) => {
  try {
    const storeId = parseInt(req.params.id, 10);
    const store = await Store.findByPk(storeId);
    if (!store) {
      return res.status(404).json({ error: 'Store not found' });
    }

    // Ensure store has a slug
    if (!store.slug) {
      const slugified = store.name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '') || `store-${store.id}`;
      await store.update({ slug: `${slugified}-${store.id}` });
    }

    // Fetch store-specific product settings
    const storeProducts = await StoreProduct.findAll({ where: { store_id: storeId } });
    const storeProductMap = {};
    storeProducts.forEach((sp) => {
      storeProductMap[sp.product_id] = {
        isPublished: sp.isPublished,
        isFeatured: sp.isFeatured,
      };
    });

    // Fetch all active products
    const products = await Product.findAll({
      where: { status: 'Active' },
      include: [
        { model: Category, as: 'Category', attributes: ['name'] },
        { model: Brand, as: 'Brand', attributes: ['name'] },
        { model: ProductVariant, as: 'ProductVariants', attributes: ['price'] },
      ],
      order: [['name', 'ASC']],
    });

    const formattedProducts = products.map((p) => {
      const obj = p.toJSON();
      const sp = storeProductMap[obj.id] || {};
      const firstPrice = obj.ProductVariants?.[0]?.price || 0;

      return {
        id: obj.id,
        name: obj.name,
        category: obj.Category?.name || '',
        brand: obj.Brand?.name || '',
        price: Number(firstPrice),
        image: obj.image || '',
        isPublished: sp.isPublished ?? obj.isPublished ?? false,
        isFeatured: sp.isFeatured ?? obj.isFeatured ?? false,
      };
    });

    res.json({
      store: store.toJSON(),
      products: formattedProducts,
    });
  } catch (error) {
    console.error('GET /api/stores/:id/showcase error:', error);
    res.status(500).json({ error: error.message });
  }
});

// ─── PUT /api/stores/:id/showcase ─── Update store public profile
router.put('/:id/showcase', authenticateToken, requirePermission('stores', 'edit'), async (req, res) => {
  try {
    const storeId = parseInt(req.params.id, 10);
    const store = await Store.findByPk(storeId);
    if (!store) {
      return res.status(404).json({ error: 'Store not found' });
    }

    const { slug, logo, banner, description, phone, openingHours, isPublicEnabled } = req.body;

    // Validate slug uniqueness if changed
    if (slug && slug.trim() !== store.slug) {
      const cleanSlug = slug.toLowerCase().trim().replace(/\s+/g, '-').replace(/[^\w\-]+/g, '');
      const existing = await Store.findOne({ where: { slug: cleanSlug } });
      if (existing && existing.id !== storeId) {
        return res.status(400).json({ error: 'This shop URL slug is already taken.' });
      }
      store.slug = cleanSlug;
    }

    if (logo !== undefined) store.logo = logo;
    if (banner !== undefined) store.banner = banner;
    if (description !== undefined) store.description = description;
    if (phone !== undefined) store.phone = phone;
    if (openingHours !== undefined) store.openingHours = openingHours;
    if (isPublicEnabled !== undefined) store.isPublicEnabled = Boolean(isPublicEnabled);

    await store.save();
    res.json(store.toJSON());
  } catch (error) {
    console.error('PUT /api/stores/:id/showcase error:', error);
    res.status(500).json({ error: error.message });
  }
});

// ─── POST /api/stores/:id/showcase/toggle-product ─── Toggle isPublished or isFeatured per product
router.post('/:id/showcase/toggle-product', authenticateToken, requirePermission('products', 'edit'), async (req, res) => {
  try {
    const storeId = parseInt(req.params.id, 10);
    const { productId, field, value } = req.body; // field: 'isPublished' | 'isFeatured'

    if (!productId || !['isPublished', 'isFeatured'].includes(field)) {
      return res.status(400).json({ error: 'Invalid productId or field name.' });
    }

    let [sp, created] = await StoreProduct.findOrCreate({
      where: { store_id: storeId, product_id: productId },
      defaults: { store_id: storeId, product_id: productId, isPublished: false, isFeatured: false },
    });

    sp[field] = Boolean(value);
    await sp.save();

    // Sync back to Product model global attribute if needed
    const product = await Product.findByPk(productId);
    if (product) {
      if (field === 'isPublished') product.isPublished = Boolean(value);
      if (field === 'isFeatured') product.isFeatured = Boolean(value);
      await product.save();
    }

    res.json({ message: 'Product showcase settings updated.', storeProduct: sp.toJSON() });
  } catch (error) {
    console.error('POST /api/stores/:id/showcase/toggle-product error:', error);
    res.status(500).json({ error: error.message });
  }
});

// ─── POST /api/stores/:id/showcase/batch-publish ─── Batch publish/unpublish products
router.post('/:id/showcase/batch-publish', authenticateToken, requirePermission('products', 'edit'), async (req, res) => {
  try {
    const storeId = parseInt(req.params.id, 10);
    const { publishAll, productIds } = req.body; // publishAll: boolean, productIds: array of IDs

    if (publishAll !== undefined) {
      const activeProducts = await Product.findAll({ where: { status: 'Active' }, attributes: ['id'] });
      for (const p of activeProducts) {
        await StoreProduct.upsert({
          store_id: storeId,
          product_id: p.id,
          isPublished: Boolean(publishAll),
        });
      }
      await Product.update({ isPublished: Boolean(publishAll) }, { where: { status: 'Active' } });
      return res.json({ message: `All products ${publishAll ? 'published' : 'unpublished'} successfully.` });
    }

    if (Array.isArray(productIds)) {
      const { action } = req.body; // 'publish' or 'unpublish'
      const isPub = action === 'publish';

      for (const pid of productIds) {
        await StoreProduct.upsert({
          store_id: storeId,
          product_id: pid,
          isPublished: isPub,
        });
      }

      await Product.update({ isPublished: isPub }, { where: { id: productIds } });
      return res.json({ message: `Selected products updated successfully.` });
    }

    res.status(400).json({ error: 'Invalid batch request parameters.' });
  } catch (error) {
    console.error('POST /api/stores/:id/showcase/batch-publish error:', error);
    res.status(500).json({ error: error.message });
  }
});

export default router;
