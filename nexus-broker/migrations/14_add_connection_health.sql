-- Add health tracking fields to connections

ALTER TABLE connections 
ADD COLUMN IF NOT EXISTS last_health_check_at TIMESTAMP WITH TIME ZONE,
ADD COLUMN IF NOT EXISTS health_status VARCHAR(50) DEFAULT 'unknown';
