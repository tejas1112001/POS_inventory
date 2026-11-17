-- Industry Phase 1 item 10: thermal receipts and HSN on receipt lines
-- (docs/INDUSTRY_PHASE1_DESIGN.md §10, DB23, DB24).

-- +goose Up
-- DB23: the shop's default receipt paper (each browser may still choose).
ALTER TABLE settings ADD COLUMN receipt_paper text NOT NULL DEFAULT 'a4'
  CONSTRAINT settings_receipt_paper_check CHECK (receipt_paper IN ('a4', '80mm', '58mm'));

-- DB24: the product's HSN code at sale time (new sales only; older lines
-- stay NULL and print as before).
ALTER TABLE order_items ADD COLUMN hsn_code text CONSTRAINT order_items_hsn_code_check CHECK (hsn_code ~ '^[0-9]{2,8}$');

-- +goose Down
ALTER TABLE order_items DROP COLUMN hsn_code;
ALTER TABLE settings DROP COLUMN receipt_paper;
