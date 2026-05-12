# Deploy in Five Minutes

This guide gets a Nexus stack running locally. By the end you will have a Broker, a Gateway, and a PostgreSQL database running in Docker, with the admin API accessible and ready to accept provider registrations.

---

## Prerequisites

You need Docker and Docker Compose installed. You also need `openssl` available on your PATH to generate the required keys.

---

## Generate the required secrets

Nexus requires two symmetric keys before it will start. Generate them now and keep them safe.

```bash
openssl rand -base64 32   # ENCRYPTION_KEY
openssl rand -base64 32   # STATE_KEY
```

The `ENCRYPTION_KEY` encrypts all stored tokens. If you lose it, all existing connections become permanently unreadable. The `STATE_KEY` signs OAuth state parameters. Both the Broker and the Gateway must receive the same `STATE_KEY` value or every OAuth callback will fail.

---

## Configure the environment

Copy the example environment file and fill in the values you just generated.

```bash
cp .env.example .env
```

Open `.env` and set:

```bash
ENCRYPTION_KEY=<your first openssl output>
STATE_KEY=<your second openssl output>
API_KEY=<any strong random string for the admin key>
```

The other variables in `.env.example` have sensible defaults for local development.

---

## Start the stack

```bash
make up
```

If you do not have `make` installed:

```bash
docker-compose up -d --build
```

This starts the Broker on port 8080 and the Gateway on port 8090. PostgreSQL and Redis start as dependencies of the Broker. The Gateway connects to the Broker automatically using the `BROKER_API_KEY` you set.

Wait a few seconds for the database migrations to complete, then verify both services are healthy:

```bash
curl http://localhost:8080/health
curl http://localhost:8090/health
```

Both should return `{"status": "ok"}`.

---

## Register your first provider

With the stack running, register a provider. This example uses Google with OIDC discovery:

```bash
curl -s -X POST http://localhost:8080/providers \
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
  }'
```

A successful registration returns the provider object with a UUID. Save the `name` field. That is the alias you use in all subsequent operations.

---

## What is next

Your stack is running and you have a provider registered. The [Environment Variables](configuration.md) page documents every configuration option the Broker and Gateway accept. The [Your First Connection](first-connection.md) page walks through completing an OAuth handshake and retrieving a credential from an agent.

For production deployment on Azure Container Apps, see the Production Deployment section of the [Environment Variables](configuration.md) page.
