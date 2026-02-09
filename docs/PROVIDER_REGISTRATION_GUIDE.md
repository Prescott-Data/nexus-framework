# Provider Registration Guide

## 1. Core Concepts

### What is a Provider?
A **Provider** is any external service (e.g., Google, Slack, Airtable) that the Dromos platform connects to on behalf of a user.

### Types of Providers
We support two primary authentication models:
1.  **OAuth2 (`oauth2`)**: Standard Redirection Flow (Google, GitHub).
2.  **Static Credentials**: Manual entry of API Keys (`api_key`), User/Pass (`basic_auth`), or AWS Keys (`aws_sigv4`).

---

## 2. OAuth2 Providers (`oauth2`)

### Step 1: Provider Console Setup
Before touching Dromos, you must create an "App" in the Provider's Developer Portal.

> **Tip:** Search Google for `[Provider Name] OAuth2 endpoints`.
> You need two URLs:
> 1.  **Auth URL:** e.g., `https://provider.com/oauth/authorize`
> 2.  **Token URL:** e.g., `https://provider.com/oauth/token`
>
> **OIDC Discovery (Self-Configuring):**
> If the provider supports OIDC (e.g., Google, Okta, Microsoft), you can often just provide the **Issuer URL** (e.g., `https://accounts.google.com`) as the `auth_url` and include `openid` in the scopes. The system will automatically find the correct endpoints via `/.well-known/openid-configuration`.

1.  **Create App:** Look for "OAuth Apps", "Integrations", or "API Credentials".
2.  **Configure Redirect URI:**
    *   This is CRITICAL. It must match exactly.
    *   **Value:** `https://nexus-broker.bravesea-3f5f7e75.eastus.azurecontainerapps.io/auth/callback`
3.  **Compliance Fields:**
    *   Most providers will strictly limit your app (making it "Development Mode" only) until you fill these out:
    *   **App Name:** `Dromos`
    *   **App Logo:** (Use Company Logo)
    *   **Website URL:** `https://prescottdata.io/`
    *   **Privacy Policy URL:** `https://support.prescottdata.io/privacy`
    *   **Terms of Service URL:** `https://support.prescottdata.io/terms`
    *   **Support Email:** `support@prescottdata.io`
4.  **Credentials:**
    *   Locate the **Client ID** and **Client Secret**.
    *   Copy them securely.

### Step 2: Registration Command
Use this template to register the provider in Dromos.

```bash
GATEWAY_URL="https://nexus-gateway.bravesea-3f5f7e75.eastus.azurecontainerapps.io"

jq -n '{
  profile: {
    name: "[provider-slug]",
    auth_type: "oauth2",
    auth_url: "[auth_url]",     # Or Issuer URL for OIDC
    token_url: "[token_url]",   # Optional if Issuer provided
    client_id: "<your_client_id>",
    client_secret: "<your_client_secret>",
    scopes: [
      "scope1", "scope2"
    ],
    api_base_url: "[api_base_url]",
    user_info_endpoint: "[user_info_endpoint]"
    # auth_header: "client_secret_basic" # Uncomment if Basic Auth required (e.g. Twitter)
  }
}' | curl -s -X POST "$GATEWAY_URL/v1/providers" \
     -H "Content-Type: application/json" \
     -d @- | jq .
```

### Step 3: Verification
Generate a connection URL to test the flow.

```bash
curl -s -X POST "$GATEWAY_URL/v1/request-connection" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "test-user-001",
    "provider_name": "[provider-slug]",
    "scopes": ["scope1", "scope2"],
    "return_url": "https://dromos-frontend-staging.bravesea-3f5f7e75.eastus.azurecontainerapps.io/app-launcher/connected-apps/all-connected-apps/" 
    # ^ Client App URL. httpbin.org is used to visualize the success params.
  }' | jq .
```
*Action:* Copy the `authUrl` from the response, paste in browser, login, and verify redirect to your app.

---

## 3. Non-OAuth Providers (API Key / Static)

These providers do not require a Console App. You define a **Schema** for the form fields.

### Option A: API Key (`api_key`)
*Single token field.*

```bash
GATEWAY_URL="https://nexus-gateway.bravesea-3f5f7e75.eastus.azurecontainerapps.io"

jq -n '{
  profile: {
    name: "[slug]-api",
    auth_type: "api_key",
    params: {
      credential_schema: {
        type: "object",
        required: ["api_key"],
        properties: {
          api_key: { type: "string", title: "Personal Access Token" }
        }
      }
    }
  }
}' | curl -s -X POST "$GATEWAY_URL/v1/providers" \
     -H "Content-Type: application/json" \
     -d @- | jq .
```

### Option B: Basic Auth (`basic_auth`)
*Username and Password.*

```bash
GATEWAY_URL="https://nexus-gateway.bravesea-3f5f7e75.eastus.azurecontainerapps.io"

jq -n '{
  profile: {
    name: "[slug]-basic",
    auth_type: "basic_auth",
    params: {
      credential_schema: {
        type: "object",
        required: ["username", "password"],
        properties: {
          username: { type: "string", title: "Username" },
          password: { type: "string", title: "Password", format: "password" }
        }
      }
    }
  }
}' | curl -s -X POST "$GATEWAY_URL/v1/providers" \
     -H "Content-Type: application/json" \
     -d @- | jq .
```

### Option C: AWS SigV4 (`aws_sigv4`)
*AWS Access Key & Secret Key.*

```bash
GATEWAY_URL="https://nexus-gateway.bravesea-3f5f7e75.eastus.azurecontainerapps.io"

jq -n '{
  profile: {
    name: "[slug]-aws",
    auth_type: "aws_sigv4",
    params: {
      service: "execute-api",
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
}' | curl -s -X POST "$GATEWAY_URL/v1/providers" \
     -H "Content-Type: application/json" \
     -d @- | jq .
```

### Option D: Query Param (`query_param`)
*API Key injected into URL query string.*

```bash
GATEWAY_URL="https://nexus-gateway.bravesea-3f5f7e75.eastus.azurecontainerapps.io"

jq -n '{
  profile: {
    name: "[slug]-query",
    auth_type: "query_param",
    params: {
      param_name: "api_token",
      credential_schema: {
        type: "object",
        required: ["api_key"],
        properties: {
          api_key: { type: "string", title: "API Token" }
        }
      }
    }
  }
}' | curl -s -X POST "$GATEWAY_URL/v1/providers" \
     -H "Content-Type: application/json" \
     -d @- | jq .
```

### Option E: HMAC Signature (`hmac_payload`)
*Request signing with a secret.*

```bash
GATEWAY_URL="https://nexus-gateway.bravesea-3f5f7e75.eastus.azurecontainerapps.io"

jq -n '{
  profile: {
    name: "[slug]-hmac",
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
}' | curl -s -X POST "$GATEWAY_URL/v1/providers" \
     -H "Content-Type: application/json" \
     -d @- | jq .
```

---

```

### Step 2: Verification (API Flow)
Non-OAuth providers do not use a browser redirect. You must verify them via API.

1.  **Get Schema URL (`/v1/request-connection`):**
    ```bash
    PROVIDER_ID="<provider_id>" # from registration response
    AUTH_URL=$(curl -s -X POST "$GATEWAY_URL/v1/request-connection" \
      -H "Content-Type: application/json" \
      -d '{
        "user_id": "test-user-123",
        "provider_id": "'$PROVIDER_ID'",
        "return_url": "https://dromos-frontend-staging.bravesea-3f5f7e75.eastus.azurecontainerapps.io/app-launcher/connected-apps/all-connected-apps/"
      }' | jq -r .authUrl)
    ```

2.  **Submit Credentials (`/auth/capture-credential`):**
    Extract `state` from the URL and submit the API Key. 
    *Note: This request hits the Broker directly as defined in the `authUrl`.*
    ```bash
    BROKER_URL="https://nexus-broker.bravesea-3f5f7e75.eastus.azurecontainerapps.io"
    STATE=$(echo "$AUTH_URL" | grep -o 'state=[^&]*' | cut -d= -f2)
    
    curl -v -X POST "$BROKER_URL/auth/capture-credential" \
      -H "Content-Type: application/json" \
      -d '{
        "state": "'$STATE'",
        "credentials": {
          "api_key": "test-token-123" # Must match schema property name
        }
      }'
    ```
    *Action:* Verify you get a `302 Found` redirect to your `return_url`.

---

## 4. Updates & Maintenance (Surgical Edits)

Do **NOT** delete and re-create a provider just to change a secret or scope. This breaks existing connections.
Use `PATCH` to update specific fields.

**1. Find Provider ID:**
```bash
# Finds the ID for provider "[slug]"
curl -s "$GATEWAY_URL/v1/providers" | jq -r '.. | .["[slug]"]? | .id // empty' | head -n 1
```

**2. Update Command (Rotate Secret):**
```bash
PROVIDER_ID="<provider-uuid>"
GATEWAY_URL="https://nexus-gateway.bravesea-3f5f7e75.eastus.azurecontainerapps.io"

curl -s -X PATCH "$GATEWAY_URL/v1/providers/$PROVIDER_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "client_secret": "NEW_SECRET_VALUE",
    "scopes": ["tweet.read", "users.read", "offline.access"]
  }' | jq .
```

---

## 5. Integration Metadata (Frontend Config)

To help your frontend render dynamic integration lists (icons, names, fields), use the Metadata endpoint.

**Command:**
```bash
curl -s "$GATEWAY_URL/v1/providers/metadata" | jq .
```

**Response:**
Returns a map grouped by type:
```json
{
  "oauth2": { "google": { "scopes": ["openid"] } },
  "api_key": { "freedcamp": { "api_base_url": "..." } }
}
```
