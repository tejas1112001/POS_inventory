-- Step 7: products, variants, store publication, inventory, stock ledger
-- (S1) and POS locks (C-15). PHASE1_DESIGN §4.4, §5.

-- +goose Up
CREATE TABLE products (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
  description text CHECK (length(description) <= 5000),
  slug citext CHECK (slug IS NULL OR length(slug) <= 200),
  status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  image_file_id uuid REFERENCES files(id),
  selling_type text CHECK (selling_type IN ('Online','POS')),
  product_type text NOT NULL CHECK (product_type IN ('single','variable')),
  tax_type text CHECK (tax_type IN ('Exclusive','Inclusive')),
  tax_rate numeric(6,3) CHECK (tax_rate BETWEEN 0 AND 100),
  hsn_code text CHECK (hsn_code ~ '^[0-9]{2,8}$'),
  discount_type text CHECK (discount_type IN ('Percentage','Fixed')),
  discount_value numeric(18,4) CHECK (discount_value >= 0),
  warranties text CHECK (length(warranties) <= 500),
  barcode_symbology text CHECK (length(barcode_symbology) <= 40),
  category_id bigint REFERENCES categories(id),
  sub_category_id bigint REFERENCES sub_categories(id),
  brand_id bigint REFERENCES brands(id),
  unit_id bigint REFERENCES units(id),
  supplier_id bigint REFERENCES suppliers(id),
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  CHECK (discount_type <> 'Percentage' OR discount_value <= 100)
);
CREATE INDEX products_active_idx ON products (status, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX products_name_trgm ON products USING gin (name gin_trgm_ops);
CREATE INDEX products_category_idx ON products (category_id);
CREATE INDEX products_brand_idx ON products (brand_id);

CREATE TABLE product_variants (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  product_id bigint NOT NULL REFERENCES products(id),
  sku citext NOT NULL CHECK (length(sku) BETWEEN 1 AND 64),
  item_barcode text CHECK (item_barcode ~ '^[A-Za-z0-9._/+-]{1,64}$'),
  price numeric(18,4) NOT NULL CHECK (price >= 0),
  cost numeric(18,4) NOT NULL DEFAULT 0 CHECK (cost >= 0),
  weight numeric(12,3) NOT NULL DEFAULT 0 CHECK (weight >= 0),
  attributes jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(attributes) = 'object'),
  expiry_date date, manufactured_date date,
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
-- One live variant per SKU and per barcode in the tenant (P1-19).
CREATE UNIQUE INDEX variants_sku_uq ON product_variants (sku) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX variants_barcode_uq ON product_variants (item_barcode) WHERE deleted_at IS NULL AND item_barcode IS NOT NULL;
CREATE INDEX variants_product_idx ON product_variants (product_id);

-- Publication is per store (C-18); there is no global flag any more.
CREATE TABLE store_products (
  store_id bigint NOT NULL REFERENCES stores(id),
  product_id bigint NOT NULL REFERENCES products(id),
  is_published boolean NOT NULL DEFAULT false,
  is_featured boolean NOT NULL DEFAULT false,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (store_id, product_id)
);
CREATE INDEX store_products_published_idx ON store_products (store_id) WHERE is_published;

CREATE TABLE inventories (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  variant_id bigint NOT NULL REFERENCES product_variants(id),
  store_id bigint NOT NULL REFERENCES stores(id),
  qty integer NOT NULL DEFAULT 0 CHECK (qty >= 0),          -- last line of defence (P0-4)
  quantity_alert integer NOT NULL DEFAULT 0 CHECK (quantity_alert >= 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (store_id, variant_id)
);
CREATE INDEX inventories_variant_idx ON inventories (variant_id);

-- Append-only stock ledger (S1): every qty change writes one row.
CREATE TABLE stock_movements (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  store_id bigint NOT NULL REFERENCES stores(id),
  variant_id bigint NOT NULL REFERENCES product_variants(id),
  delta integer NOT NULL CHECK (delta <> 0),
  qty_after integer NOT NULL CHECK (qty_after >= 0),
  reason text NOT NULL CHECK (reason IN ('opening','sale','sale_edit','sale_void','return',
          'purchase_receive','adjustment','manual_set','product_edit','import')),
  ref_type text, ref_id bigint,
  user_id bigint REFERENCES users(id),
  impersonation_id uuid,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX stock_movements_item_idx ON stock_movements (store_id, variant_id, created_at DESC);
CREATE INDEX stock_movements_ref_idx ON stock_movements (ref_type, ref_id);

-- Advisory POS locks per (store, product), 2-minute TTL (C-15).
CREATE TABLE pos_locks (
  store_id bigint NOT NULL REFERENCES stores(id),
  product_id bigint NOT NULL REFERENCES products(id),
  user_id bigint NOT NULL REFERENCES users(id),
  locked_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  PRIMARY KEY (store_id, product_id)
);
CREATE INDEX pos_locks_expiry_idx ON pos_locks (expires_at);

-- +goose Down
DROP TABLE pos_locks;
DROP TABLE stock_movements;
DROP TABLE inventories;
DROP TABLE store_products;
DROP TABLE product_variants;
DROP TABLE products;
