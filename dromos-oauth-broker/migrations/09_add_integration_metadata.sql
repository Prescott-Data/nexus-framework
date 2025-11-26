-- Add metadata columns for API integration
-- api_base_url: The root URL for the provider's API (e.g., "https://api.github.com")
-- user_info_endpoint: The path relative to base_url to fetch user profile (e.g., "/user")

ALTER TABLE provider_profiles 
ADD COLUMN IF NOT EXISTS api_base_url TEXT,
ADD COLUMN IF NOT EXISTS user_info_endpoint TEXT;

-- Add comments for clarity
COMMENT ON COLUMN provider_profiles.api_base_url IS 'Root URL for the provider API';
COMMENT ON COLUMN provider_profiles.user_info_endpoint IS 'Path to fetch user profile info';
