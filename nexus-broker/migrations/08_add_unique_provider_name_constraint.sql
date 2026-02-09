-- Add unique constraint to provider_profiles.name for non-deleted records
-- This ensures no two active providers can have the same name
-- Also normalizes all names to kebab-case (lowercase with hyphens)

-- First, normalize all existing provider names to kebab-case
UPDATE provider_profiles
SET name = LOWER(
    REGEXP_REPLACE(
        REGEXP_REPLACE(TRIM(name), '[^a-zA-Z0-9]+', '-', 'g'),
        '^-+|-+$', '', 'g'
    )
)
WHERE deleted_at IS NULL;

-- Now handle any duplicates that may have been created by normalization
-- Keep the most recently created provider for each duplicate name
WITH ranked_providers AS (
    SELECT 
        id,
        name,
        created_at,
        ROW_NUMBER() OVER (PARTITION BY name ORDER BY created_at DESC) as rn
    FROM provider_profiles
    WHERE deleted_at IS NULL
)
UPDATE provider_profiles
SET deleted_at = NOW()
WHERE id IN (
    SELECT id FROM ranked_providers WHERE rn > 1
);

-- Now add a unique partial index that enforces uniqueness for non-deleted providers
-- A partial index is used because we only want uniqueness when deleted_at IS NULL
CREATE UNIQUE INDEX idx_provider_profiles_name_unique 
ON provider_profiles (name) 
WHERE deleted_at IS NULL;

-- Add a comment to document this constraint
COMMENT ON INDEX idx_provider_profiles_name_unique IS 
'Ensures provider names are unique among non-deleted providers. Allows reusing names after soft deletion.';
