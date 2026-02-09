-- OAuth Broker Database Schema

-- Enable pgcrypto for gen_random_uuid()
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Provider profiles store OAuth provider configurations
CREATE TABLE provider_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    client_id VARCHAR(255) NOT NULL,
    client_secret TEXT NOT NULL,
    auth_url TEXT NOT NULL,
    token_url TEXT NOT NULL,
    scopes TEXT[], -- Default scopes
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Connections track OAuth flows in progress and completed
CREATE TABLE connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id VARCHAR(255) NOT NULL,
    provider_id UUID NOT NULL REFERENCES provider_profiles(id),
    status VARCHAR(50) NOT NULL DEFAULT 'pending', -- pending, active, failed
    code_verifier TEXT NOT NULL, -- PKCE code verifier
    scopes TEXT[], -- Requested scopes
    return_url TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() + INTERVAL '10 minutes'), -- TTL for pending connections
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Tokens store encrypted OAuth tokens
CREATE TABLE tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID NOT NULL REFERENCES connections(id),
    encrypted_data TEXT NOT NULL, -- AES-GCM encrypted JSON
    expires_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Audit events for security and debugging
CREATE TABLE audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    connection_id UUID REFERENCES connections(id),
    event_type VARCHAR(100) NOT NULL,
    event_data JSONB,
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Indexes for performance
CREATE INDEX idx_connections_workspace_provider ON connections(workspace_id, provider_id);
CREATE INDEX idx_connections_status_expires ON connections(status, expires_at);
CREATE INDEX idx_tokens_connection ON tokens(connection_id);
CREATE INDEX idx_audit_connection ON audit_events(connection_id);
CREATE INDEX idx_audit_event_type ON audit_events(event_type);
