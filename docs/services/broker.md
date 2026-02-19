# Nexus Broker

The **Nexus Broker** is the secure heart of the Nexus Framework. It serves as the centralized vault for identity management, orchestrating authentication flows, encrypting sensitive credentials, and managing the lifecycle of user connections.

It is designed to be deployed as an **internal-only service**, protected from the public internet, with only specific endpoints exposed for user callbacks.

## 1. Core Architecture

The Broker operates as the "System of Record" for all identity data. It sits between the **Nexus Gateway** and the upstream **Identity Providers** (Google, GitHub, Salesforce, etc.).

### Responsibilities

1.  **Provider Registry:** Stores configuration for all supported integrations (Client IDs, Scopes, API endpoints).
2.  **The Handshake Engine:** Generates cryptographically secure authorization URLs and validates callbacks using `state` and `nonce` protection.
3.  **Token Vault:** Encrypts all long-lived credentials (Refresh Tokens, API Keys) at rest using AES-256-GCM.
4.  **Secret Rotation:** Automatically refreshes OAuth tokens before they expire to ensure uninterrupted agent access.
5.  **Policy Enforcement:** Validates IP allowlists and API keys for all incoming administrative requests.

### Data Flow

```mermaid
graph TD
    User -->|Consent| Broker(Public Callback)
    Broker -->|Exchange Code| Provider(Google/GitHub)
    Broker -->|Encrypt & Store| DB[(PostgreSQL)]
    
    Gateway -->|Request Token| Broker(Internal API)
    Broker -->|Decrypt| DB
    Broker -->|Refresh if needed| Provider
    Broker -->|Return Access Token| Gateway
```

---

## 2. Authentication Flows

The Broker supports two distinct classes of authentication, handling the complexity of each so agents don't have to.

### A. OAuth 2.0 / OIDC (Standard)
For providers that support modern standards (e.g., Google, Microsoft, Okta).
- **Discovery:** Automatically fetches endpoints and JWKS from the Issuer URL.
- **PKCE:** Enforces Proof Key for Code Exchange for enhanced security.
- **State Validation:** Signs the `state` parameter to prevent CSRF and Replay attacks.
- **Token Management:** Handles Access Token, Refresh Token, and ID Token verification.

### B. Static Credentials (Non-OAuth)
For legacy or simple API providers (e.g., Stripe API Key, AWS Credentials).
- **Schema Driven:** You define a JSON schema for the credentials (e.g., "Enter API Key").
- **Secure Capture:** The Broker provides endpoints to capture these credentials securely from a frontend.
- **Standardized Usage:** The Broker stores these encrypted and returns them to the Bridge in a normalized format, so agents consume them just like OAuth tokens.

---

## 3. Configuration & Deployment

The Broker is a 12-factor application configured entirely via environment variables.

### Critical Security Variables

| Variable | Required | Description |
| :--- | :--- | :--- |
| `ENCRYPTION_KEY` | **Yes** | A 32-byte Base64-encoded string used for AES-GCM encryption of the database. **Loss of this key results in permanent data loss.** |
| `STATE_KEY` | **Yes** | A 32-byte Base64-encoded string used to sign OIDC state parameters. Must match the Gateway's key. |
| `API_KEY` | **Yes** | The internal secret used to authenticate the Gateway and Admin tools. |

### Infrastructure Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | The HTTP port to listen on. |
| `DATABASE_URL` | - | PostgreSQL connection string (e.g., `postgres://user:pass@host:5432/db`). |
| `REDIS_URL` | - | Redis connection string for caching and temporary state (e.g., `redis://localhost:6379`). |
| `BASE_URL` | - | The public URL of the Broker (used for constructing redirect URIs). |
| `ALLOWED_CIDRS` | `0.0.0.0/0` | Comma-separated list of CIDRs allowed to access the API. **Set this to your Gateway's IP in production.** |

### Running with Docker

```bash
docker run -d \
  -p 8080:8080 \
  -e ENCRYPTION_KEY="..." \
  -e STATE_KEY="..." \
  -e API_KEY="..." \
  -e DATABASE_URL="..." \
  -e REDIS_URL="..." \
  nexus-broker:latest
```

---

## 4. Operational Maintenance

### Database Migrations
The Broker uses SQL migrations to manage its schema. These run automatically on startup in development, but for production, you should run them explicitly.

```bash
# Run migrations using the built-in tool
./nexus-broker migrate up
```

### Health Checks
- **Liveness:** `GET /health` returns 200 OK if the service is running.
- **Readiness:** The service checks connectivity to PostgreSQL and Redis on startup.

### Key Rotation
- **API_KEY:** Can be rotated at any time by updating the environment variable on both the Broker and Gateway.
- **ENCRYPTION_KEY:** **Cannot be easily rotated.** Rotation requires a script to decrypt all data with the old key and re-encrypt with the new key. (Feature planned).

---

## 5. Security & "Secret Zero"

The Broker is the only component in the Nexus Framework that ever holds a **Master Secret** (Refresh Token) in plaintext memory, and only for the brief milliseconds required to perform a token exchange.

1.  **Rest:** Secrets are encrypted in PostgreSQL.
2.  **Transit (Internal):** Secrets are decrypted and sent to the Provider over HTTPS.
3.  **Transit (Downstream):** The Broker **never** sends the Refresh Token to the Gateway. It only returns the short-lived Access Token (Usage Secret).

This architecture ensures that a compromise of the Gateway, Bridge, or Agent does not lead to persistent account takeover.
