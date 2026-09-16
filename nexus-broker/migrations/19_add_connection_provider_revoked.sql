-- Records whether the upstream RFC 7009 revocation succeeded, so a retried
-- revocation reports the original outcome instead of inventing a fresh one.
-- NULL until a revocation actually runs.
--
-- This belongs with 18_add_connection_revocation.sql and is only separate
-- because the migration ledger is keyed by filename with no checksum: any
-- database that already applied 18 would silently skip an edited copy of it
-- and end up without this column. Adding it as its own file means those
-- databases pick it up too.

ALTER TABLE connections ADD COLUMN IF NOT EXISTS provider_revoked BOOLEAN;
