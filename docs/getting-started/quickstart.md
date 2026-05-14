---
icon: material/rocket-launch-outline
---

# Deploy in Five Minutes

This guide gets a Nexus stack running locally. By the end you will have a Broker, a Gateway, PostgreSQL, and Redis running in Docker, with the admin API accessible and ready to accept provider registrations.

---

## Prerequisites

- Docker and Docker Compose installed
- `openssl` on your PATH

---

## Step 1: Generate secrets

Nexus requires two symmetric keys before it will start. Generate them now.

```bash
openssl rand -base64 32   # ENCRYPTION_KEY
openssl rand -base64 32   # STATE_KEY — run separately, do not reuse the same value
```

**ENCRYPTION_KEY** encrypts all stored tokens using AES-GCM 256-bit. If this key is lost or rotated while connections exist, every stored token becomes permanently unreadable. Treat it like a master key and back it up accordingly.

**STATE_KEY** signs OAuth state parameters to prevent CSRF attacks. Both the Broker and the Gateway must receive the same value, or every OAuth callback will fail with a state mismatch.

---

## Step 2: Configure the environment

```bash
cp .env.example .env
```

Open `.env` and set the following fields:

```bash
ENCRYPTION_KEY=<your first openssl output>
STATE_KEY=<your second openssl output>
API_KEY=<a strong random string — this is your admin key for the Broker>
BROKER_API_KEY=<a different strong random string — the Gateway uses this to talk to the Broker>
```

The remaining variables in `.env.example` have sensible defaults for local development. `BASE_URL` defaults to `http://localhost:8080`, which means the OAuth callback URL is `http://localhost:8080/auth/callback`. Register that URI in your provider's developer console.

---

## Step 3: Start the stack

```bash
make up
```

If you do not have `make` installed:

```bash
docker-compose up -d --build
```

This builds and starts four containers:

| Container | Port | Purpose |
|---|---|---|
| `nexus-broker` | 8080 | Stores tokens, runs OAuth flows, background refresh |
| `nexus-gateway` | 8090 | Public API — your agents and backend call this |
| `nexus-postgres` | 5432 | Encrypted token storage |
| `nexus-redis` | 6379 | Caching and state |

Wait for migrations to complete (a few seconds), then verify both services are healthy:

```bash
curl http://localhost:8080/health   # {"status": "ok"}
curl http://localhost:8090/health   # {"status": "ok"}
```

---

## Step 4: Register your first provider

Provider registration goes through the **Gateway** at port 8090. This example registers Google with OIDC discovery:

```bash
curl -s -X POST http://localhost:8090/v1/providers \
  -H "Content-Type: application/json" \
  -H "X-API-Key: <your API_KEY>" \
  -d '{
    "name": "google",
    "auth_type": "oauth2",
    "client_id": "YOUR_GOOGLE_CLIENT_ID",
    "client_secret": "YOUR_GOOGLE_CLIENT_SECRET",
    "issuer": "https://accounts.google.com",
    "enable_discovery": true,
    "scopes": ["openid", "email", "profile", "offline_access"]
  }' | jq .
```

A successful registration returns the provider object with a UUID. The `name` field (`google`) is the alias you use in all subsequent operations.

---

## What is next

Your stack is running and you have a provider registered. Continue to [Your First Connection](first-connection.md) to walk through the full OAuth handshake and retrieve your first credential.

For all configuration options, see [Configuration](configuration.md). For production deployment on Docker, Kubernetes, or Azure Container Apps, see [Deploying Nexus](../infrastructure/deploying-nexus.md).
