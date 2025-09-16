-- Add issuer and enable_discovery to provider_profiles
ALTER TABLE provider_profiles
    ADD COLUMN IF NOT EXISTS issuer TEXT,
    ADD COLUMN IF NOT EXISTS enable_discovery BOOLEAN NOT NULL DEFAULT FALSE;

-- Touch updated_at on change
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_provider_profiles_updated_at ON provider_profiles;
CREATE TRIGGER trg_provider_profiles_updated_at
BEFORE UPDATE ON provider_profiles
FOR EACH ROW EXECUTE FUNCTION set_updated_at();


