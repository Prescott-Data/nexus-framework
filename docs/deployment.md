# Deployment & Configuration

## Configuration

### Shared
- `STATE_KEY` **(REQUIRED**, base64‑32B): HMAC signing for `state` and nonce binding. **Must match between Gateway and Broker.** Services will refuse to start if this is missing or invalid. Generate with `openssl rand -base64 32`.

### Broker (nexus-broker)
Required — the broker will not start without these:
- `DATABASE_URL` (PostgreSQL connection string).
- `BASE_URL` (Public URL, e.g., `https://broker.example.com`).
- `ENCRYPTION_KEY` **(REQUIRED**, base64‑32B): AES‑GCM key for token encryption. **Must be stable.** If this key is lost, all stored connections become permanently unrecoverable. Generate with `openssl rand -base64 32`.
- `REDIRECT_PATH` (Default `/auth/callback`).
- `API_KEY`: Key required for internal API access.
- `ALLOWED_CIDRS`: Comma-separated list of allowed IP ranges (e.g., `10.0.0.0/8`).
- `ALLOWED_RETURN_DOMAINS`: Comma-separated list of allowed domains for return URLs.

### Gateway (nexus-gateway)
- `PORT`: Service port.
- `BROKER_BASE_URL`: URL of the Broker (internal if possible).
- `STATE_KEY` **(REQUIRED)**: Same as Broker — must match exactly.
- `BROKER_API_KEY`: Key to authenticate with the Broker.

## Shared Secrets Management

The Nexus Framework relies on two primary symmetric keys. Proper management of these keys is critical for security and availability.

### 1. `ENCRYPTION_KEY` (AES-256-GCM)
Used to encrypt decrypted OAuth tokens before they are stored in the database.
- **Risk**: If this key is changed or lost, all existing connections in the database will become unreadable. You will be forced to rotate the key and ask all users to re-authenticate.
- **Guidance**: Store this in a secure vault (Azure Key Vault, AWS Secrets Manager, HashiCorp Vault). It should be stable across deployments.

### 2. `STATE_KEY` (HMAC-SHA256)
Used to sign the `state` parameter during the initial redirect and verify it on callback.
- **Risk**: If the Broker and Gateway have different keys, all OAuth callbacks will fail with "Invalid state" errors.
- **Guidance**: Both the Broker and Gateway instances must receive the exact same value. In orchestrated environments (Kubernetes, Docker Swarm), use a shared Secret object.

## Local Development (Quickstart)

### 0. Generate required keys
```bash
# Run once, paste outputs into your .env file
openssl rand -base64 32   # → ENCRYPTION_KEY
openssl rand -base64 32   # → STATE_KEY (must be the same in Broker and Gateway)
```

### 1. Run the Broker
```bash
cp .env.example .env
# Edit .env — fill in ENCRYPTION_KEY and STATE_KEY from step 0
cd nexus-broker
source .env
go run ./cmd/nexus-broker
```

### 2. Run the Gateway
```bash
cd nexus-gateway
source ../.env   # reuse the same STATE_KEY
export BROKER_BASE_URL="http://localhost:8080"
go run ./cmd/nexus-rest
```

## Production Deployment (Azure Container Apps)

1.  **Build & Push**: Build Docker images for `nexus-broker` and `nexus-gateway` and push to ACR.
2.  **Networking**:
    - The **Broker** must be reachable from the public internet for OAuth callbacks (unless using a front-door proxy).
    - The **Gateway** can be internal-only if only accessed by internal agents, or public if agents are external.
3.  **Env Vars**: Set the environment variables defined above in the Container App configuration.
4.  **Database**: Ensure the Broker can connect to the PostgreSQL instance (Managed Identity or connection string).
