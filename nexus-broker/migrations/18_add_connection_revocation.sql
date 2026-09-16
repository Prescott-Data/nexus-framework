-- Connection revocation.
--
-- A revoked connection is terminal: its token row is deleted outright (the
-- credential must not survive the revocation anywhere) and the row is kept
-- only as an auditable tombstone. revoked_at records when that happened and
-- revocation_reason records who/what asked for it.

ALTER TABLE connections ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ;
ALTER TABLE connections ADD COLUMN IF NOT EXISTS revocation_reason TEXT;

-- Revoked connections are excluded from every "usable connection" lookup, so
-- the partial index keeps those scans off the tombstones.
CREATE INDEX IF NOT EXISTS idx_connections_revoked_at
    ON connections(revoked_at) WHERE revoked_at IS NOT NULL;
