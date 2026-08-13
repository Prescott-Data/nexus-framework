-- Add SAML Service Provider configuration fields to provider profiles.

ALTER TABLE provider_profiles
ADD COLUMN IF NOT EXISTS saml_idp_entity_id TEXT,
ADD COLUMN IF NOT EXISTS saml_idp_sso_url TEXT,
ADD COLUMN IF NOT EXISTS saml_idp_x509_cert TEXT,
ADD COLUMN IF NOT EXISTS saml_sp_entity_id TEXT;
