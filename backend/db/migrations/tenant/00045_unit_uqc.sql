-- GST reports (docs/GST_REPORTS_DESIGN.md DB1): a unit's GST Unique
-- Quantity Code (NOS, KGS, BOX, ...) for the HSN summary of GSTR-1. The
-- list lives in code (internal/gst); a unit without one reports as OTH.

-- +goose Up
ALTER TABLE units ADD COLUMN uqc text CHECK (uqc ~ '^[A-Z]{3}$');

-- +goose Down
ALTER TABLE units DROP COLUMN uqc;
