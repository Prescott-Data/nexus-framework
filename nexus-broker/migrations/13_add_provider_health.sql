-- Add health tracking fields to provider_profiles

ALTER TABLE provider_profiles 
ADD COLUMN last_health_check_at TIMESTAMP WITH TIME ZONE,
ADD COLUMN health_status VARCHAR(50) DEFAULT 'unknown',
ADD COLUMN health_message TEXT;
