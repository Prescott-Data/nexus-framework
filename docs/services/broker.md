# Nexus Broker

The **Nexus Broker** is the core engine of the framework. It acts as the central authority for managing identity providers and establishing secure connections.

## Core Responsibilities

### 1. Provider Management
The Broker manages the lifecycle of **Providers**. 
- **OAuth2/OIDC:** Supports discovery-based configuration using Issuer URLs.
- **Static Keys:** Allows defining JSON schemas for API keys, AWS credentials, and more.
- **Aliases:** Maps human-readable names (e.g., "google-prod") to internal UUIDs.

### 2. The Handshake Engine
The Broker orchestrates the complex dance of user consent.
- **State Management:** Generates and validates OIDC `state` and `nonce` parameters using the `STATE_KEY`.
- **PKCE Support:** Automatically generates and validates Proof Key for Code Exchange (PKCE) challenges.
- **Callback Handling:** Receives the provider's code, exchanges it for a token, and handles the user redirection back to the agent.

### 3. Token Vault (Security)
The Broker is the only service that touches sensitive "Master Secrets" (Refresh Tokens).
- **At-Rest Encryption:** Every token stored in the database is encrypted using **AES-GCM 256-bit**.
- **The Master Key:** Encryption relies on the `ENCRYPTION_KEY` environment variable. If this key is lost, all stored connections become unrecoverable.
- **Secret Zero:** The Broker never sends Refresh Tokens to the Gateway; it only sends the short-lived Access Tokens and Usage Secrets.

### 4. Background Refresh Loop
To ensure agents never face a "cold start" due to expired tokens:
- The Broker continuously monitors tokens nearing expiry.
- It performs background refreshes using stored Refresh Tokens.
- If a refresh fails permanently (e.g., user revoked access), it transitions the connection to `attention_required`.

### 5. Audit Subsystem
Every control-plane mutation is recorded in the `audit_events` table via the `audit.Service`:
- **`provider.created`** — logged on every successful `POST /providers` call.
- **`provider.updated`** — logged on `PUT` and `PATCH` mutations.
- **`provider.deleted`** — logged on deletion by ID or by name.
- **`oauth_flow_completed`** — logged on every successful OAuth callback (token exchange + storage).
- **`token_exchange_failed`**, **`token_storage_failed`**, etc. — logged on callback failures.
- **`token_retrieved`** — logged on every successful `GET /connections/{id}/token` call.
- **`token_refresh_fatal`** — logged when a token refresh fails permanently (4xx from provider).

Audit events capture the **caller IP** (respecting `X-Forwarded-For`), **User-Agent**, and structured **event data** (provider ID, name, etc.).

See the [Audit Log Reference](../reference/audit-log.md) for how to query events.

## Environment Variables

| Variable | Description | Default |
| :--- | :--- | :--- |
| `DATABASE_URL` | PostgreSQL connection string. | Required |
| `REDIS_URL` | Redis URL for caching discovery and state. | Required |
| `ENCRYPTION_KEY` | 32-byte Base64 key for AES-GCM. | Required |
| `STATE_KEY` | 32-byte Base64 key for signing state. Must match the Gateway. The Broker will **fatal-exit** on startup if absent. | Required |
| `API_KEY` | Key for Gateway-to-Broker authentication. | Required |

