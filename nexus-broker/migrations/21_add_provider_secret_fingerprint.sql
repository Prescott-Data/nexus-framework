-- Non-reversible fingerprint (truncated SHA-256) of a provider's client_secret,
-- returned by the API so nexus-cli can tell whether the secret in a manifest
-- differs from the one stored, without the secret ever being read back.
-- NULL for rows written before this column existed; filled in on the next
-- rotation, or when an execution path resolves the secret.
ALTER TABLE provider_profiles ADD COLUMN IF NOT EXISTS client_secret_fingerprint TEXT;
