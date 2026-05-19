-- Add composite index for health check query performance
-- Covers the WHERE status = 'active' AND last_health_check_at < ... pattern
-- used by GetForHealthCheck

CREATE INDEX IF NOT EXISTS idx_connections_health_check
ON connections (status, last_health_check_at ASC NULLS FIRST)
WHERE status = 'active';
