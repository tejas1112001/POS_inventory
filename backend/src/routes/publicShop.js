import express from 'express';
import { Op } from 'sequelize';
import {
  Store,
  Product,
  ProductVariant,
  Inventory,
  Category,
  Brand,
  Unit,
  StoreProduct,
} from '../models/index.js';

const router = express.Router();

/**
 * Helper to transform product data into a public payload.
 * STRICTLY STRIPS: cost, supplier_id, supplier, locked_by, created_by, internal alerts.
 */
function formatPublicProduct(p, storeId, storeProductMap = {}) {
  const obj = typeof p.toJSON === 'function' ? p.toJSON() : p;
  const storeSetting = storeProductMap[obj.id] || {};

  const variants = (obj.ProductVariants || []).map((v) => {
    // Filter inventories for this specific store
    const storeInventories = (v.Inventories || []).filter(
      (i) => !storeId || String(i.store_id) === String(storeId)
    );
    const variantQty = storeInventories.reduce((sum, i) => sum + Number(i.qty || 0), 0);

    return {
      id: v.id,
      sku: v.sku,
      itemBarcode: v.itemBarcode,
      price: Number(v.price || 0),
      attributes: v.attributes || {},
      weight: Number(v.weight || 0),
      inStock: variantQty > 0,
      stockStatus: variantQty > 0 ? 'In Stock' : 'Out of Stock',
      qty: variantQty,
    };
  });

  const firstVariant = variants[0] || null;
  const totalQty = variants.reduce((sum, v) => sum + v.qty, 0);

  // Calculate discounted price if applicable
  const basePrice = firstVariant ? firstVariant.price : 0;
  let finalPrice = basePrice;
  let hasDiscount = false;

  if (obj.discountType && obj.discountValue > 0) {
    if (obj.discountType === 'Percentage') {
      finalPrice = Math.max(0, basePrice - (basePrice * obj.discountValue) / 100);
      hasDiscount = true;
    } else if (obj.discountType === 'Fixed') {
      finalPrice = Math.max(0, basePrice - obj.discountValue);
      hasDiscount = true;
    }
  }

  return {
    id: obj.id,
    name: obj.name,
    description: obj.description || '',
    image: obj.image || '',
    slug: obj.slug || '',
    productType: obj.productType,
    category: obj.Category?.name || '',
    categoryId: obj.category_id,
    brand: obj.Brand?.name || '',
    brandId: obj.brand_id,
    unit: obj.Unit?.name || '',
    price: basePrice,
    finalPrice: Math.round(finalPrice * 100) / 100,
    hasDiscount,
    discountType: obj.discountType || null,
    discountValue: obj.discountValue || 0,
    taxType: obj.taxType || null,
    tax: obj.tax || 0,
    warranties: obj.warranties || '',
    isPublished: storeSetting.isPublished ?? obj.isPublished ?? true,
    isFeatured: storeSetting.isFeatured ?? obj.isFeatured ?? false,
    inStock: totalQty > 0,
    stockStatus: totalQty > 0 ? 'In Stock' : 'Out of Stock',
    totalQty,
    variants,
  };
}

// ─── GET /api/public/shop/:slug ─── Store Public Info
router.get('/:slug', async (req, res) => {
  try {
    const { slug } = req.params;
    const store = await Store.findOne({
      where: {
        [Op.or]: [{ slug }, { id: isNaN(slug) ? -1 : parseInt(slug) }],
        status: 'Active',
      },
    });

    if (!store) {
      return res.status(404).json({ error: 'Shop not found or inactive.' });
    }

    const storeData = store.toJSON();

    if (!storeData.isPublicEnabled) {
      return res.json({
        id: storeData.id,
        name: storeData.name,
        slug: storeData.slug,
        isPublicEnabled: false,
        message: 'This shop showcase is currently unavailable.',
      });
    }

    res.json({
      id: storeData.id,
      name: storeData.name,
      slug: storeData.slug,
      address: storeData.address || '',
      logo: storeData.logo || '',
      banner: storeData.banner || '',
      description: storeData.description || '',
      phone: storeData.phone || '',
      openingHours: storeData.openingHours || '',
      isPublicEnabled: true,
    });
  } catch (error) {
    console.error('GET /api/public/shop/:slug error:', error);
    res.status(500).json({ error: 'Failed to load shop information.' });
  }
});

// ─── GET /api/public/shop/:slug/products ─── Public Products Listing
router.get('/:slug/products', async (req, res) => {
  try {
    const { slug } = req.params;
    const { page, limit, search, categoryId, brandId, sort, featured } = req.query;

    const store = await Store.findOne({
      where: {
        [Op.or]: [{ slug }, { id: isNaN(slug) ? -1 : parseInt(slug) }],
        status: 'Active',
      },
    });

    if (!store || !store.isPublicEnabled) {
      return res.status(404).json({ error: 'Shop showcase is not available.' });
    }

    const storeId = store.id;

    // Fetch store-specific product settings
    const storeProducts = await StoreProduct.findAll({
      where: { store_id: storeId },
    });

    const storeProductMap = {};
    const publishedProductIds = new Set();
    const featuredProductIds = new Set();

    storeProducts.forEach((sp) => {
      storeProductMap[sp.product_id] = {
        isPublished: sp.isPublished,
        isFeatured: sp.isFeatured,
      };
      if (sp.isPublished) publishedProductIds.add(sp.product_id);
      if (sp.isFeatured) featuredProductIds.add(sp.product_id);
    });

    // Build product filter
    const productWhere = { status: 'Active' };

    if (categoryId) productWhere.category_id = categoryId;
    if (brandId) productWhere.brand_id = brandId;

    if (search && search.trim()) {
      productWhere.name = { [Op.iLike || Op.like]: `%${search.trim()}%` };
    }

    // Published filter logic:
    // Either published specifically in store_products OR globally marked as isPublished
    const publishedCondition = [
      { isPublished: true },
    ];
    if (publishedProductIds.size > 0) {
      publishedCondition.push({ id: Array.from(publishedProductIds) });
    }
    productWhere[Op.or] = publishedCondition;

    if (featured === 'true') {
      const featuredCondition = [{ isFeatured: true }];
      if (featuredProductIds.size > 0) {
        featuredCondition.push({ id: Array.from(featuredProductIds) });
      }
      productWhere[Op.and] = [{ [Op.or]: featuredCondition }];
    }

    // Determine sorting
    let order = [['createdAt', 'DESC']];
    if (sort === 'price_asc' || sort === 'price_desc') {
      order = [['createdAt', sort === 'price_asc' ? 'ASC' : 'DESC']];
    } else if (sort === 'name_asc') {
      order = [['name', 'ASC']];
    } else if (sort === 'newest') {
      order = [['createdAt', 'DESC']];
    }

    const includes = [
      { model: Category, as: 'Category', attributes: ['id', 'name'] },
      { model: Brand, as: 'Brand', attributes: ['id', 'name'] },
      { model: Unit, as: 'Unit', attributes: ['id', 'name'] },
      {
        model: ProductVariant,
        as: 'ProductVariants',
        attributes: ['id', 'sku', 'itemBarcode', 'price', 'attributes', 'weight'],
        include: [
          {
            model: Inventory,
            as: 'Inventories',
            attributes: ['qty', 'store_id'],
            where: { store_id: storeId },
            required: false,
          },
        ],
      },
    ];

    const pageNum = Math.max(1, parseInt(page) || 1);
    const limitNum = Math.max(1, parseInt(limit) || 24);
    const offset = (pageNum - 1) * limitNum;

    const { count, rows } = await Product.findAndCountAll({
      where: productWhere,
      include: includes,
      limit: limitNum,
      offset,
      order,
      distinct: true,
    });

    const formatted = rows.map((p) => formatPublicProduct(p, storeId, storeProductMap));

    // Secondary client sort for price if variable pricing
    if (sort === 'price_asc') {
      formatted.sort((a, b) => a.finalPrice - b.finalPrice);
    } else if (sort === 'price_desc') {
      formatted.sort((a, b) => b.finalPrice - a.finalPrice);
    }

    res.json({
      data: formatted,
      pagination: {
        total: count,
        page: pageNum,
        limit: limitNum,
        totalPages: Math.ceil(count / limitNum),
      },
    });
  } catch (error) {
    console.error('GET /api/public/shop/:slug/products error:', error);
    res.status(500).json({ error: 'Failed to retrieve products.' });
  }
});

// ─── GET /api/public/shop/:slug/products/:productId ─── Public Product Details
router.get('/:slug/products/:productId', async (req, res) => {
  try {
    const { slug, productId } = req.params;

    const store = await Store.findOne({
      where: {
        [Op.or]: [{ slug }, { id: isNaN(slug) ? -1 : parseInt(slug) }],
        status: 'Active',
      },
    });

    if (!store || !store.isPublicEnabled) {
      return res.status(404).json({ error: 'Shop showcase unavailable.' });
    }

    const productWhere = {
      status: 'Active',
      [Op.or]: [{ id: isNaN(productId) ? -1 : parseInt(productId) }, { slug: productId }],
    };

    const product = await Product.findOne({
      where: productWhere,
      include: [
        { model: Category, as: 'Category', attributes: ['id', 'name'] },
        { model: Brand, as: 'Brand', attributes: ['id', 'name'] },
        { model: Unit, as: 'Unit', attributes: ['id', 'name'] },
        {
          model: ProductVariant,
          as: 'ProductVariants',
          attributes: ['id', 'sku', 'itemBarcode', 'price', 'attributes', 'weight'],
          include: [
            {
              model: Inventory,
              as: 'Inventories',
              attributes: ['qty', 'store_id'],
              where: { store_id: store.id },
              required: false,
            },
          ],
        },
      ],
    });

    if (!product) {
      return res.status(404).json({ error: 'Product not found.' });
    }

    // Check store-specific published state
    const storeProduct = await StoreProduct.findOne({
      where: { store_id: store.id, product_id: product.id },
    });

    const isPub = storeProduct ? storeProduct.isPublished : product.isPublished;

    if (!isPub && process.env.NODE_ENV !== 'development') {
      return res.status(404).json({ error: 'Product is not available for public view.' });
    }

    const storeProductMap = {
      [product.id]: storeProduct ? storeProduct.toJSON() : { isPublished: isPub },
    };

    const formatted = formatPublicProduct(product, store.id, storeProductMap);

    res.json({
      product: formatted,
      shop: {
        id: store.id,
        name: store.name,
        slug: store.slug,
        phone: store.phone || '',
        address: store.address || '',
        openingHours: store.openingHours || '',
      },
    });
  } catch (error) {
    console.error('GET /api/public/shop/:slug/products/:productId error:', error);
    res.status(500).json({ error: 'Failed to retrieve product details.' });
  }
});

// ─── GET /api/public/shop/:slug/categories ─── Public Categories
router.get('/:slug/categories', async (req, res) => {
  try {
    const { slug } = req.params;

    const store = await Store.findOne({
      where: {
        [Op.or]: [{ slug }, { id: isNaN(slug) ? -1 : parseInt(slug) }],
        status: 'Active',
      },
    });

    if (!store || !store.isPublicEnabled) {
      return res.status(404).json({ error: 'Shop showcase unavailable.' });
    }

    const categories = await Category.findAll({
      attributes: ['id', 'name'],
      order: [['name', 'ASC']],
    });

    res.json(categories);
  } catch (error) {
    console.error('GET /api/public/shop/:slug/categories error:', error);
    res.status(500).json({ error: 'Failed to load categories.' });
  }
});

// ─── GET /api/public/shop/:slug/featured ─── Public Featured Products
router.get('/:slug/featured', async (req, res) => {
  try {
    const { slug } = req.params;

    const store = await Store.findOne({
      where: {
        [Op.or]: [{ slug }, { id: isNaN(slug) ? -1 : parseInt(slug) }],
        status: 'Active',
      },
    });

    if (!store || !store.isPublicEnabled) {
      return res.status(404).json({ error: 'Shop showcase unavailable.' });
    }

    const storeProducts = await StoreProduct.findAll({
      where: { store_id: store.id, isFeatured: true, isPublished: true },
    });

    const featuredIds = storeProducts.map((sp) => sp.product_id);

    const products = await Product.findAll({
      where: {
        status: 'Active',
        [Op.or]: [
          { isFeatured: true, isPublished: true },
          ...(featuredIds.length > 0 ? [{ id: featuredIds }] : []),
        ],
      },
      include: [
        { model: Category, as: 'Category', attributes: ['id', 'name'] },
        { model: Brand, as: 'Brand', attributes: ['id', 'name'] },
        {
          model: ProductVariant,
          as: 'ProductVariants',
          attributes: ['id', 'sku', 'price'],
          include: [
            {
              model: Inventory,
              as: 'Inventories',
              attributes: ['qty', 'store_id'],
              where: { store_id: store.id },
              required: false,
            },
          ],
        },
      ],
      limit: 8,
    });

    const formatted = products.map((p) => formatPublicProduct(p, store.id));
    res.json(formatted);
  } catch (error) {
    console.error('GET /api/public/shop/:slug/featured error:', error);
    res.status(500).json({ error: 'Failed to load featured products.' });
  }
});

export default router;
