-- Migration: Enforce one token row per connection via UPSERT-friendly unique constraint.
-- Fixes issue #25: token rows accumulate unboundedly on every refresh.
--
-- Step 1: Deduplicate — keep only the most recent token per connection_id.
-- This DELETE can be slow on large tables; run during low-traffic windows if needed.
DELETE FROM tokens t1
USING tokens t2
WHERE t1.connection_id = t2.connection_id
  AND t1.created_at < t2.created_at;

-- Step 2: Add unique constraint so only one token row per connection can exist.
-- INSERT ... ON CONFLICT (connection_id) DO UPDATE will use this constraint.
ALTER TABLE tokens
    ADD CONSTRAINT tokens_connection_id_unique UNIQUE (connection_id);
