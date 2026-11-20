-- Industry Phase 1 item 4 (docs/INDUSTRY_PHASE1_DESIGN.md, DB5-DB8): typed
-- attributes and the variant matrix. Every column has a default that keeps
-- today's products unchanged: no product-level attributes, no variant
-- dimensions (so variants keep their free, unvalidated attributes), no
-- article number, and no variant label on older sale lines.

-- +goose Up
CREATE TABLE attribute_defs (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  key citext NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{0,39}$'),
  label text NOT NULL CHECK (length(label) BETWEEN 1 AND 60),
  level text NOT NULL CHECK (level IN ('product', 'variant')),
  kind text NOT NULL CHECK (kind IN ('choice', 'text')),
  choices jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(choices) = 'array' AND jsonb_array_length(choices) <= 200),
  sort integer NOT NULL DEFAULT 0 CHECK (sort BETWEEN 0 AND 10000),
  status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active', 'Inactive')),
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX attribute_defs_key_uq ON attribute_defs (key) WHERE deleted_at IS NULL;

ALTER TABLE products
  ADD COLUMN attributes jsonb NOT NULL DEFAULT '{}' CONSTRAINT products_attributes_check CHECK (jsonb_typeof(attributes) = 'object'),
  ADD COLUMN variant_dims text[] NOT NULL DEFAULT '{}' CONSTRAINT products_variant_dims_check CHECK (cardinality(variant_dims) <= 4 AND array_position(variant_dims, NULL) IS NULL),
  ADD COLUMN article_no text CONSTRAINT products_article_no_check CHECK (length(article_no) BETWEEN 1 AND 64);
CREATE INDEX products_article_no_idx ON products (lower(article_no)) WHERE article_no IS NOT NULL;

ALTER TABLE order_items ADD COLUMN variant_label text CONSTRAINT order_items_variant_label_check CHECK (length(variant_label) <= 200);

-- +goose Down
ALTER TABLE order_items DROP COLUMN variant_label;
DROP INDEX products_article_no_idx;
ALTER TABLE products DROP COLUMN article_no, DROP COLUMN variant_dims, DROP COLUMN attributes;
DROP TABLE attribute_defs;
