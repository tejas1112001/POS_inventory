-- Step 6: files and master data (PHASE1_DESIGN §5, API_CONTRACT §2.2-2.3, §5.1).
-- Unqualified names; runs with search_path = "t_<id>", extensions.

-- +goose Up
CREATE TABLE files (
  id uuid PRIMARY KEY,
  kind text NOT NULL CHECK (kind IN ('image','receipt','export')),
  -- Only tenant_logo, store_logo, store_banner and product_image can ever be
  -- served on the public /files/t/ route (API_CONTRACT §5.1). Every other
  -- purpose is served behind login with a permission on the owning entity.
  purpose text NOT NULL CHECK (purpose IN ('tenant_logo','store_logo','store_banner','product_image',
                                          'customer_image','user_image','category_image','brand_image',
                                          'supplier_image','receipt','export')),
  content_type text NOT NULL CHECK (content_type IN ('image/png','image/jpeg','application/pdf','application/octet-stream')),
  size_bytes bigint NOT NULL CHECK (size_bytes > 0),
  sha256 bytea NOT NULL CHECK (length(sha256) = 32),
  storage_key text NOT NULL UNIQUE,
  width int, height int,
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);

CREATE TABLE categories (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name citext NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  image_file_id uuid REFERENCES files(id),
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX categories_name_uq ON categories (name) WHERE deleted_at IS NULL;

CREATE TABLE sub_categories (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name citext NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  category_id bigint NOT NULL REFERENCES categories(id),
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX sub_categories_name_uq ON sub_categories (category_id, name) WHERE deleted_at IS NULL;
CREATE INDEX sub_categories_category_idx ON sub_categories (category_id);

CREATE TABLE brands (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name citext NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  image_file_id uuid REFERENCES files(id),
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX brands_name_uq ON brands (name) WHERE deleted_at IS NULL;

CREATE TABLE units (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name citext NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
  short_name text NOT NULL DEFAULT '' CHECK (length(short_name) <= 20),
  status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX units_name_uq ON units (name) WHERE deleted_at IS NULL;

CREATE TABLE suppliers (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  email citext CHECK (length(email) <= 254),
  phone text CHECK (length(phone) <= 40),
  address text CHECK (length(address) <= 500),
  image_file_id uuid REFERENCES files(id),
  status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE INDEX suppliers_name_trgm ON suppliers USING gin (name gin_trgm_ops);

CREATE TABLE warehouses (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  address text CHECK (length(address) <= 500),
  status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);

ALTER TABLE customers ADD CONSTRAINT customers_image_fk FOREIGN KEY (image_file_id) REFERENCES files(id);
ALTER TABLE customers ADD CONSTRAINT customers_lengths CHECK (
  length(coalesce(phone, '')) <= 40 AND length(coalesce(address, '')) <= 500
  AND length(coalesce(city, '')) <= 120 AND length(coalesce(country, '')) <= 120 AND length(coalesce(email::text, '')) <= 254);
ALTER TABLE stores ADD CONSTRAINT stores_logo_fk FOREIGN KEY (logo_file_id) REFERENCES files(id);
ALTER TABLE stores ADD CONSTRAINT stores_banner_fk FOREIGN KEY (banner_file_id) REFERENCES files(id);
ALTER TABLE settings ADD CONSTRAINT settings_logo_fk FOREIGN KEY (logo_file_id) REFERENCES files(id);

-- +goose Down
ALTER TABLE settings DROP CONSTRAINT settings_logo_fk;
ALTER TABLE stores DROP CONSTRAINT stores_banner_fk;
ALTER TABLE stores DROP CONSTRAINT stores_logo_fk;
ALTER TABLE customers DROP CONSTRAINT customers_lengths;
ALTER TABLE customers DROP CONSTRAINT customers_image_fk;
DROP TABLE warehouses;
DROP TABLE suppliers;
DROP TABLE units;
DROP TABLE brands;
DROP TABLE sub_categories;
DROP TABLE categories;
DROP TABLE files;
