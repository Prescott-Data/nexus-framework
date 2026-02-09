# Deployment & Configuration

## Configuration

### Shared
- `STATE_KEY` (base64‑32B): HMAC signing for `state` and nonce binding. **Must match between Gateway and Broker.**

### Broker (nexus-broker)
Required for production:
- `DATABASE_URL` (PostgreSQL connection string).
- `BASE_URL` (Public URL, e.g., `https://broker.example.com`).
- `REDIRECT_PATH` (Default `/auth/callback`).
- `ENCRYPTION_KEY` (base64‑32B): AES‑GCM key for token encryption. **Must be stable.**
- `API_KEY`: Key required for internal API access.
- `ALLOWED_CIDRS`: Comma-separated list of allowed IP ranges (e.g., `10.0.0.0/8`).
- `ALLOWED_RETURN_DOMAINS`: Comma-separated list of allowed domains for return URLs.

### Gateway (nexus-gateway)
- `PORT`: Service port.
- `BROKER_BASE_URL`: URL of the Broker (internal if possible).
- `STATE_KEY`: Same as Broker.
- `BROKER_API_KEY`: Key to authenticate with the Broker.

## Local Development (Quickstart)

### 1. Run the Broker
```bash
cd nexus-broker
# Create a .env file based on .env.example
source .env 
go run ./cmd/nexus-broker
```

### 2. Run the Gateway
```bash
cd nexus-gateway
export BROKER_BASE_URL="http://localhost:8080"
export STATE_KEY="$(openssl rand -base64 32)" # Use the same key as the Broker
go run ./cmd/oha
```

## Production Deployment (Azure Container Apps)

1.  **Build & Push**: Build Docker images for `nexus-broker` and `nexus-gateway` and push to ACR.
2.  **Networking**:
    - The **Broker** must be reachable from the public internet for OAuth callbacks (unless using a front-door proxy).
    - The **Gateway** can be internal-only if only accessed by internal agents, or public if agents are external.
3.  **Env Vars**: Set the environment variables defined above in the Container App configuration.
4.  **Database**: Ensure the Broker can connect to the PostgreSQL instance (Managed Identity or connection string).
