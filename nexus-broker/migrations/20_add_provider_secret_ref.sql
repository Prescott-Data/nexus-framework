-- Immutable reference to the backend secret version a provider row uses
-- (SECRET_BACKEND != internal). Written in the same statement as the rest
-- of the row on registration and rotation, so a reader always pairs the
-- metadata with the matching secret version. NULL for rows written before
-- this column existed; those keep reading the backend's current version.
ALTER TABLE provider_profiles ADD COLUMN IF NOT EXISTS client_secret_ref TEXT;
