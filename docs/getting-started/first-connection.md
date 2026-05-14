---
icon: material/lan-connect
---

# Your First Connection

This walkthrough takes you from a running Nexus stack with a registered provider to a working credential retrieval. It uses the Google provider registered in the [quickstart](quickstart.md) and covers each step of the OAuth handshake through to the first token fetch.

By the end you will have a `connection_id` and know exactly how to use it to retrieve credentials from your application or agent.

---

## What your application is responsible for

Before walking through the flow, establish the division of responsibility. Nexus stores OAuth tokens, manages refresh, and handles the provider handshake. Your application stores exactly one thing: a `connection_id`.

The `connection_id` is an opaque string that references a user's authorized connection with a specific provider. You persist it in your own database, associated with your user record. When your agent needs credentials, it presents the `connection_id` to the Gateway and receives a short-lived access token. That is the full integration surface.

Your application never sees a refresh token. It never handles token expiry. It never implements provider-specific auth logic.

---

## Step 1: Initiate the connection

Your backend calls the Gateway to create a pending connection. Pass the workspace identifier for the user, the provider name, the scopes needed, and a `return_url` on your frontend where Nexus will redirect the user after consent.

```bash
curl -s -X POST http://localhost:8090/v1/request-connection \
  -H "Content-Type: application/json" \
  -H "X-API-Key: <your API_KEY>" \
  -d '{
    "workspace_id": "user_abc123",
    "provider_name": "google",
    "scopes": ["openid", "email", "profile"],
    "return_url": "https://app.example.com/oauth/return"
  }' | jq .
```

The response:

```json
{
  "auth_url": "https://accounts.google.com/o/oauth2/auth?client_id=...&state=...",
  "connection_id": "conn_01HXYZ..."
}
```

Store the `connection_id` immediately, associated with `user_abc123`. Then redirect the user's browser to `auth_url`.

---

## Step 2: The user completes consent

The user lands on Google's consent screen, selects the account they want to connect, and grants the requested permissions. Google redirects the browser to the Broker's callback endpoint (`BASE_URL/auth/callback`).

The Broker validates the `state` parameter against the signed value it issued in step 1, exchanges the authorization code for access and refresh tokens, encrypts the tokens with `ENCRYPTION_KEY`, and stores them in PostgreSQL.

The Broker then redirects the user's browser to your `return_url` with query parameters:

```
https://app.example.com/oauth/return?status=success&connection_id=conn_01HXYZ...
```

Your frontend extracts `connection_id` from the query string and confirms it against what your backend stored in step 1.

---

## Step 3: Verify the connection is active

Check the connection status to confirm the handshake completed successfully before presenting the connection to the user as ready:

```bash
curl -s http://localhost:8090/v1/check-connection/conn_01HXYZ... | jq .
```

Response when active:

```json
{
  "status": "active"
}
```

| Status | Meaning |
|---|---|
| `pending` | User has not yet completed consent |
| `active` | Tokens stored, connection is usable |
| `attention_required` | Refresh failed — user must reconnect |
| `failed` | Initial token exchange failed |

---

## Step 4: Retrieve credentials

Once the connection is active, retrieve credentials by calling the Gateway:

```bash
curl -s http://localhost:8090/v1/token/conn_01HXYZ... \
  -H "X-API-Key: <your API_KEY>" | jq .
```

Response:

```json
{
  "strategy": { "type": "oauth2" },
  "credentials": {
    "access_token": "ya29.A0AfH6...",
    "expires_at": 1715000000
  },
  "expires_at": 1715000000
}
```

Inspect `strategy.type` to know how to apply the credentials:

| Strategy type | How to use |
|---|---|
| `oauth2` | `Authorization: Bearer <access_token>` header |
| `api_key` | Header or query param as defined in the provider's `credential_schema` |
| `basic_auth` | `Authorization: Basic <base64(username:password)>` |
| `aws_sigv4` | AWS Signature Version 4 — the Bridge handles this automatically |

The Bridge and all three SDKs handle strategy interpretation and credential injection automatically. See [Integrating Agents](../guides/integrating-agents.md) for implementation examples in Go, TypeScript, and Python.

---

## Handling attention_required in production

If `/v1/token/{connection_id}` returns a non-200 response with `"status": "attention_required"`, the provider rejected the last refresh attempt — typically because the user revoked the application's access. Your application should surface this to the user and prompt them to go through the consent flow again. The new flow creates a fresh connection with a new `connection_id`.

Do not retry automatically. A provider rejection is not a transient error.
