-- Add health tracking fields to connections

ALTER TABLE connections 
ADD COLUMN last_health_check_at TIMESTAMP WITH TIME ZONE,
ADD COLUMN health_status VARCHAR(50) DEFAULT 'unknown';
