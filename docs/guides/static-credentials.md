---
icon: material/key-outline
---

# Static Credential Flow

OAuth2 providers redirect users to an authorization page. Static credential providers — `api_key` and `basic_auth` — do not. Instead, your application presents a form, the user fills it in, and you submit the credentials directly to Nexus.

This guide covers the complete flow for static credential connections.

## How it works

```
Your backend                    Gateway                  Broker
     │                              │                       │
     │  POST /v1/request-connection │                       │
     │─────────────────────────────►│                       │
     │  ◄── { auth_url, connection_id }                     │
     │                              │                       │
     │  GET /v1/capture-schema      │                       │
     │─────────────────────────────►│                       │
     │  ◄── { fields schema }       │                       │
     │                              │                       │
     │  (render form, user fills in credentials)            │
     │                              │                       │
     │  POST /v1/capture-credential │                       │
     │─────────────────────────────►│──────────────────────►│
     │  ◄── { connection_id, status: "success" }            │
     │                              │                       │
     │  GET /v1/check-connection    │                       │
     │─────────────────────────────►│                       │
     │  ◄── { status: "active" }    │                       │
```

## Step 1 — Initiate the connection

Call `POST /v1/request-connection` as you would for an OAuth2 provider. The response contains an `auth_url` with a signed `state` token. You will need the state for the credential submission.

```bash
curl -s -X POST https://your-gateway.example.com/v1/request-connection \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-gateway-api-key" \
  -d '{
    "workspace_id": "user_abc",
    "provider_id": "PROVIDER_UUID",
    "return_url": "https://your-app.com/connections/callback"
  }' | jq .
```

Response:

```json
{
  "connection_id": "8f3a1c2d-...",
  "auth_url": "https://your-broker.example.com/auth/capture-schema?state=eyJ..."
}
```

Store the `connection_id`. Extract the `state` parameter from the `auth_url`.

## Step 2 — Fetch the credential schema

Call `GET /v1/capture-schema` with the state token to get the field definitions for this provider.

```bash
curl -s "https://your-gateway.example.com/v1/capture-schema?state=eyJ..." \
  -H "X-API-Key: your-gateway-api-key" | jq .
```

Response for an API key provider:

```json
{
  "type": "object",
  "required": ["api_key"],
  "properties": {
    "api_key": {
      "type": "string",
      "title": "Personal Access Token"
    }
  }
}
```

Response for a basic auth provider:

```json
{
  "type": "object",
  "required": ["username", "password"],
  "properties": {
    "username": { "type": "string", "title": "Username" },
    "password": { "type": "string", "title": "Password", "format": "password" }
  }
}
```

Use the schema to render a form in your application UI. The `title` field is the human-readable label. Fields with `"format": "password"` should use a masked input.

## Step 3 — Submit the credentials

When the user submits the form, post the credentials to `POST /v1/capture-credential`:

```bash
curl -s -X POST https://your-gateway.example.com/v1/capture-credential \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-gateway-api-key" \
  -d '{
    "state": "eyJ...",
    "credentials": {
      "api_key": "pat_abc123xyz"
    }
  }' | jq .
```

The Gateway submits this to the Broker, which encrypts the credentials and stores them. The response:

```json
{
  "connection_id": "8f3a1c2d-...",
  "status": "success",
  "redirect_url": "https://your-app.com/connections/callback?status=success&connection_id=8f3a1c2d-..."
}
```

The Broker also issues a redirect to the `return_url` you specified in step 1. If your application uses a browser-based flow, redirect the user there. If it is server-side only, the `connection_id` in the response is sufficient.

## Step 4 — Verify the connection is active

```bash
curl -s "https://your-gateway.example.com/v1/check-connection/8f3a1c2d-..." \
  -H "X-API-Key: your-gateway-api-key" | jq .
```

```json
{ "status": "active" }
```

## Fetching tokens for static connections

The token fetch is identical to OAuth2. Call `GET /v1/token/{connection_id}`:

```bash
curl -s "https://your-gateway.example.com/v1/token/8f3a1c2d-..." \
  -H "X-API-Key: your-gateway-api-key" | jq .
```

The `credentials` field in the response contains the stored key material. The `strategy` field tells the Bridge how to inject it into outgoing requests.

## Static connections cannot be refreshed

Calling `POST /v1/refresh/{connection_id}` on a static connection returns:

```json
{ "error": "static_token", "message": "This connection uses a static token and cannot be refreshed" }
```

To update credentials — for example when a user rotates their API key — initiate a new capture flow for the same `workspace_id` and `provider_id`. The new connection replaces the old one in your application's connection registry.
