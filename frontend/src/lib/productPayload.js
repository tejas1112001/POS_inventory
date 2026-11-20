// productUpdatePayload builds a full PUT /api/products/{id} body from a
// loaded product, so a screen that changes one part (attributes, variant
// values) leaves everything else as it is. The image is left out (kept);
// stock is left out (stores not listed keep their stock).
export function productUpdatePayload(p, patch = {}, variantAttrs = null) {
  return {
    name: p.name,
    description: p.description ?? '',
    status: p.status,
    categoryId: p.categoryId ?? null,
    subCategoryId: p.subCategoryId ?? null,
    brandId: p.brandId ?? null,
    unitId: p.unitId ?? null,
    supplierId: p.supplierId ?? null,
    productType: p.productType,
    taxType: p.taxType ?? null,
    tax: p.tax ?? null,
    hsnCode: p.hsnCode ?? null,
    discountType: p.discountType ?? null,
    discountValue: p.discountValue ?? null,
    warranties: p.warranties ?? null,
    barcodeSymbology: p.barcodeSymbology ?? null,
    sellingType: p.sellingType ?? null,
    slug: p.slug ?? null,
    allowFraction: p.allowFraction, // H3: kept as it is
    variants: (p.ProductVariants || []).map((v) => ({
      id: v.id,
      sku: v.sku,
      itemBarcode: v.itemBarcode || '',
      price: v.price,
      cost: v.cost,
      weight: v.weight,
      attributes: (variantAttrs && variantAttrs[v.id]) || v.attributes || {},
      expiryDate: v.expiryDate || null,
      manufacturedDate: v.manufacturedDate || null,
    })),
    ...patch,
  };
}
