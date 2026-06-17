-- Add SAML specific configuration fields to provider_profiles

ALTER TABLE provider_profiles
ADD COLUMN saml_idp_entity_id VARCHAR(255),
ADD COLUMN saml_idp_sso_url VARCHAR(255),
ADD COLUMN saml_idp_x509_cert TEXT,
ADD COLUMN saml_sp_entity_id VARCHAR(255);
