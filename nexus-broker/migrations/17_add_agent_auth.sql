-- Agent identity and scoped session registry.

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    description TEXT,
    allowed_scopes TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    active BOOLEAN NOT NULL DEFAULT true
);

CREATE TABLE IF NOT EXISTS agent_sessions (
    session_id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents(id),
    connection_id UUID NOT NULL REFERENCES connections(id),
    scopes_granted TEXT[] NOT NULL DEFAULT '{}',
    expires_at TIMESTAMPTZ NOT NULL,
    closed_at TIMESTAMPTZ,
    obo BOOLEAN NOT NULL DEFAULT false,
    acting_for TEXT,
    tenant_id TEXT,
    clearance_level INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_sessions_agent_id ON agent_sessions(agent_id);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_connection_id ON agent_sessions(connection_id);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_expires_at ON agent_sessions(expires_at) WHERE closed_at IS NULL;