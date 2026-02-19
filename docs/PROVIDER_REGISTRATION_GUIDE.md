# Provider Registration Guide

This guide provides practical `curl` templates for registering different types of providers with the **Nexus Broker**.

> **Note:** Provider registration is an administrative task performed directly against the **Broker API** (Internal). It requires the `X-API-Key` header.

## Prerequisites

Set these environment variables for the examples below:

```bash
# The internal URL of your Nexus Broker
export BROKER_URL="http://localhost:8080"

# Your internal API Key (Admin Key)
export API_KEY="nexus-admin-key"
```

---

## 1. OAuth2 Providers (`oauth2`)

### Step 1: Provider Console Setup
Before touching Nexus, you must create an "App" in the Provider's Developer Portal.

1.  **Callback URL:** Set the redirect URI to: `https://<your-broker-public-url>/auth/callback`
2.  **Credentials:** Obtain the `client_id` and `client_secret`.

### Step 2: Registration Command

```bash
jq -n '{
  profile: {
    name: "google",
    auth_type: "oauth2",
    issuer: "https://accounts.google.com", # Auto-configures endpoints via OIDC
    client_id: "YOUR_GOOGLE_CLIENT_ID",
    client_secret: "YOUR_GOOGLE_CLIENT_SECRET",
    scopes: ["openid", "email", "profile"],
    api_base_url: "https://www.googleapis.com",
    user_info_endpoint: "/oauth2/v3/userinfo",
    params: {
      access_type: "offline",
      prompt: "consent"
    }
  }
}' | curl -s -X POST "$BROKER_URL/providers" \
     -H "Content-Type: application/json" \
     -H "X-API-Key: $API_KEY" \
     -d @- | jq .
```

### Step 3: Verification
Generate a connection URL via the **Gateway** (Public API) to test the flow.

```bash
# Gateway URL (Public)
export GATEWAY_URL="http://localhost:8090"

curl -s -X POST "$GATEWAY_URL/v1/request-connection" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "test-user-001",
    "provider_name": "google",
    "scopes": ["openid", "email"],
    "return_url": "https://httpbin.org/get" 
  }' | jq .
```
*Action:* Copy the `authUrl` from the response, paste in browser, login, and verify redirect to httpbin.org.

---

## 2. Non-OAuth Providers (API Key / Static)

These providers do not require a Console App. You define a **Schema** for the form fields.

### Option A: API Key (`api_key`)
*Single token field.*

```bash
jq -n '{
  profile: {
    name: "stripe-api",
    auth_type: "api_key",
    params: {
      credential_schema: {
        type: "object",
        required: ["api_key"],
        properties: {
          api_key: { type: "string", title: "Stripe Secret Key" }
        }
      }
    }
  }
}' | curl -s -X POST "$BROKER_URL/providers" \
     -H "Content-Type: application/json" \
     -H "X-API-Key: $API_KEY" \
     -d @- | jq .
```

### Option B: Basic Auth (`basic_auth`)
*Username and Password.*

```bash
jq -n '{
  profile: {
    name: "jira-basic",
    auth_type: "basic_auth",
    params: {
      credential_schema: {
        type: "object",
        required: ["username", "password"],
        properties: {
          username: { type: "string", title: "Email" },
          password: { type: "string", title: "API Token", format: "password" }
        }
      }
    }
  }
}' | curl -s -X POST "$BROKER_URL/providers" \
     -H "Content-Type: application/json" \
     -H "X-API-Key: $API_KEY" \
     -d @- | jq .
```

### Option C: AWS SigV4 (`aws_sigv4`)
*AWS Access Key & Secret Key.*

```bash
jq -n '{
  profile: {
    name: "aws-s3",
    auth_type: "aws_sigv4",
    params: {
      service: "s3",
      region: "us-east-1",
      credential_schema: {
        type: "object",
        required: ["access_key", "secret_key"],
        properties: {
          access_key: { type: "string", title: "AWS Access Key ID" },
          secret_key: { type: "string", title: "AWS Secret Access Key" }
        }
      }
    }
  }
}' | curl -s -X POST "$BROKER_URL/providers" \
     -H "Content-Type: application/json" \
     -H "X-API-Key: $API_KEY" \
     -d @- | jq .
```

### Option D: Query Param (`query_param`)
*API Key injected into URL query string.*

```bash
jq -n '{
  profile: {
    name: "weather-api",
    auth_type: "query_param",
    params: {
      param_name: "appid",
      credential_schema: {
        type: "object",
        required: ["api_key"],
        properties: {
          api_key: { type: "string", title: "App ID" }
        }
      }
    }
  }
}' | curl -s -X POST "$BROKER_URL/providers" \
     -H "Content-Type: application/json" \
     -H "X-API-Key: $API_KEY" \
     -d @- | jq .
```

### Option E: HMAC Signature (`hmac_payload`)
*Request signing with a secret.*

```bash
jq -n '{
  profile: {
    name: "webhook-signer",
    auth_type: "hmac_payload",
    params: {
      header_name: "X-Signature",
      algo: "sha256",
      encoding: "hex",
      credential_schema: {
        type: "object",
        required: ["api_secret"],
        properties: {
          api_secret: { type: "string", title: "Signing Secret" }
        }
      }
    }
  }
}' | curl -s -X POST "$BROKER_URL/providers" \
     -H "Content-Type: application/json" \
     -H "X-API-Key: $API_KEY" \
     -d @- | jq .
```

---

## 3. Updates & Maintenance

Use `PATCH` to update specific fields without re-creating the provider.

**1. Find Provider ID:**
```bash
curl -s "$BROKER_URL/providers/by-name/google" \
  -H "X-API-Key: $API_KEY" | jq -r .id
```

**2. Update Command (Rotate Secret):**
```bash
PROVIDER_ID="<provider-uuid>"

curl -s -X PATCH "$BROKER_URL/providers/$PROVIDER_ID" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $API_KEY" \
  -d '{
    "client_secret": "NEW_SECRET_VALUE"
  }' | jq .
```
