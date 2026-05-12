# Your First Connection

This walkthrough takes you from a running Nexus stack and a registered provider to a working credential retrieval. It uses the Google provider registered in the [quickstart](quickstart.md) and walks through each step of the OAuth handshake and the subsequent credential fetch.

By the end you will have a `connection_id` and know how to use it to retrieve credentials from your application or agent.

---

## What your application stores

Before walking through the flow, clarify what your application's responsibility is. Nexus stores OAuth tokens. Your application stores a `connection_id`, an opaque string that references a user's authorized connection with a specific provider. You persist the `connection_id` in your own database, associated with your user. When your agent needs credentials, it presents the `connection_id` to the Gateway.

Your application never sees a refresh token. It never handles token expiry. Those are Nexus's responsibilities.

---

## Step 1: Initiate the connection

Your backend calls the Gateway to create a pending connection. Pass the user's identifier, the provider name, the scopes needed, and a `return_url` on your frontend where Nexus will redirect the user after consent.

```bash
curl -s -X POST http://localhost:8090/v1/request-connection \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user_abc123",
    "provider_name": "google",
    "scopes": ["openid", "email", "profile"],
    "return_url": "https://app.example.com/oauth/return"
  }'
```

The response includes two fields:

```json
{
  "authUrl": "https://accounts.google.com/o/oauth2/auth?client_id=...&state=...",
  "connection_id": "conn_01HXYZ..."
}
```

Store the `connection_id` immediately, associated with `user_abc123`. Then redirect the user's browser to `authUrl`.

---

## Step 2: The user completes consent

The user lands on Google's consent screen, selects the account they want to connect, and grants the requested permissions. Google redirects to the Broker's callback URL. The Broker validates the `state` parameter, exchanges the authorization code for tokens, encrypts the tokens, and stores them.

The Broker then redirects the user's browser to your `return_url` with query parameters:

```
https://app.example.com/oauth/return?status=success&connection_id=conn_01HXYZ...
```

Your frontend extracts the `connection_id` from the query string and sends it to your backend for persistence if you have not already stored it from step 1.

---

## Step 3: Verify the connection is active

You can poll the connection status before using it, particularly if your backend needs to confirm the handshake completed successfully:

```bash
curl -s http://localhost:8090/v1/check-connection/conn_01HXYZ...
```

A successful connection returns:

```json
{
  "status": "active"
}
```

If the status is `pending`, the user has not yet completed consent. If it is `failed`, the token exchange did not succeed and the user will need to reconnect.

---

## Step 4: Retrieve credentials

Once the connection is active, your agent retrieves credentials by calling the Gateway with the `connection_id`:

```bash
curl -s http://localhost:8090/v1/token/conn_01HXYZ...
```

The response:

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

Inspect `strategy.type` to know how to use the credentials. For `oauth2`, inject the `access_token` as a `Bearer` token in the `Authorization` header of your API calls to the provider. For `api_key` or `basic_auth`, the `credentials` object will contain the fields appropriate for that scheme.

The Bridge handles this step and the header injection automatically. See the [Integrating Agents](../guides/integrating-agents.md) guide if your agent is written in Go.

---

## Checking connection status in production

In production, surface connection status to your users. If a call to `/v1/token/{connection_id}` returns a non-200 response, it typically means the connection has moved to `attention_required` because the user revoked access or the provider expired the refresh token. Your application should handle this by asking the user to go through the consent flow again, which will create a new connection and update the `connection_id` you store for that user.
