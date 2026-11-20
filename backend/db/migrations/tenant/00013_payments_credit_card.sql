-- Fix-up of the 00012 backfill (S10 item 5): the old sales screens stored
-- payment_method "credit" for card payments (the button says "Card"), and
-- 00012 mapped it to cash. Only the backfilled rows of such sales change
-- (no collection rows, no cash tendered); the amounts stay the same.

-- +goose Up
UPDATE sale_payments p SET method = 'card'
FROM sales s
WHERE p.sale_id = s.id AND p.customer_payment_id IS NULL AND p.method = 'cash' AND p.tendered IS NULL
  AND lower(coalesce(s.payment_method, '')) LIKE '%credit%';

-- +goose Down
SELECT 1;
