-- Payments the owner takes outside Razorpay (D-172): cash, UPI or bank transfer collected
-- when approving a business. The order row says how it was paid and the reference given.
-- Only additions.
ALTER TABLE crm.billing_orders ADD COLUMN IF NOT EXISTS method text NOT NULL DEFAULT 'razorpay';
ALTER TABLE crm.billing_orders ADD COLUMN IF NOT EXISTS reference text;
