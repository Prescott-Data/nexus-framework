-- Add health tracking fields to provider_profiles

ALTER TABLE provider_profiles 
ADD COLUMN IF NOT EXISTS last_health_check_at TIMESTAMP WITH TIME ZONE,
ADD COLUMN IF NOT EXISTS health_status VARCHAR(50) DEFAULT 'unknown',
ADD COLUMN IF NOT EXISTS health_message TEXT;
