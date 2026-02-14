# Provider Registration and Management Guide

This guide provides a comprehensive overview of how to register, manage, and test identity providers within the Nexus OAuth Broker.

## Provider Types

The broker supports two primary types of providers:

1.  **OAuth 2.0 / OIDC Providers**: Standard identity providers like Google, Microsoft Entra ID, or Okta that use the OAuth 2.0 authorization code flow. These are typically configured using an OIDC discovery issuer URL.
2.  **Non-OAuth (API Key) Providers**: Services that use static credentials, such as API keys, tokens, or username/password combinations. These are configured using a flexible JSON schema.

---

## 1. OAuth 2.0 / OIDC Providers

### Registration

The preferred method for registering OIDC-compliant providers is to use the `issuer` URL, which enables auto-discovery of the necessary endpoints.

#### **Payload Fields:**

*   `name` (string, required): A unique name for the provider (e.g., "google").
*   `issuer` (string, optional): The OIDC issuer URL for auto-discovery.
*   `client_id` (string, required): The OAuth client ID from the provider.
*   `client_secret` (string, required): The OAuth client secret from the provider.
*   `scopes` (string array, required): A list of default scopes to request.
*   `auth_url` (string, optional): Override for the authorization endpoint.
*   `token_url` (string, optional): Override for the token endpoint.
*   `auth_header` (string, optional): Authentication method for token exchange. Values: `"client_secret_post"` (default, credentials in body) or `"client_secret_basic"` (credentials in Basic Auth header). Required for Twitter/GitHub.
*   `api_base_url` (string, optional): The root URL for the provider's API (e.g., "https://api.github.com"). Exposed to frontend for integration logic.
*   `user_info_endpoint` (string, optional): Path to fetch user profile (e.g., "/user"). Exposed to frontend.
*   `params` (json, optional): A JSON object for provider-specific parameters (e.g., `{"access_type": "offline"}`).

#### **Example: Registering Google**

```bash
curl -X POST http://localhost:8080/providers \
     -H "Content-Type: application/json" \
     -H "X-API-Key: dev-api-key-12345" \
     -d '{
        "profile": {
            "name": "google",
            "issuer": "https://accounts.google.com",
            "client_id": "YOUR_GOOGLE_CLIENT_ID",
            "client_secret": "YOUR_GOOGLE_CLIENT_SECRET",
            "scopes": ["openid", "email", "profile"],
            "api_base_url": "https://www.googleapis.com",
            "user_info_endpoint": "/oauth2/v3/userinfo",
            "params": {
                "access_type": "offline",
                "prompt": "consent"
            }
        }
     }' | jq . 
```

#### **Example: Registering Twitter (Basic Auth)**

Twitter requires `client_secret_basic` and manual endpoint configuration.

```bash
curl -X POST http://localhost:8080/providers \
     -H "Content-Type: application/json" \
     -H "X-API-Key: dev-api-key-12345" \
     -d '{
        "profile": {
            "name": "twitter",
            "auth_type": "oauth2",
            "auth_url": "https://twitter.com/i/oauth2/authorize",
            "token_url": "https://api.twitter.com/2/oauth2/token",
            "client_id": "YOUR_TWITTER_CLIENT_ID",
            "client_secret": "YOUR_TWITTER_CLIENT_SECRET",
            "scopes": ["tweet.read", "users.read"],
            "auth_header": "client_secret_basic",
            "api_base_url": "https://api.twitter.com/2",
            "user_info_endpoint": "/users/me"
        }
     }' | jq . 
```

### Testing the OAuth 2.0 Flow

You can simulate the entire flow using `curl`.

#### **Step 1: Get the Consent URL**

Request a consent specification from the broker. This is what a client application would do to start the login process.

```bash
# Replace <provider-id> with the ID returned from the registration step
PROVIDER_ID="<provider-id>"

curl -s -X POST http://localhost:8080/auth/consent-spec \
    -H "Content-Type: application/json" \
    -H "X-API-Key: dev-api-key-12345" \
    -d '{
        "workspace_id": "ws-test-123",
        "provider_id": "'$PROVIDER_ID'",
        "scopes": ["openid", "email"],
        "return_url": "http://localhost:3000/my-app-callback"
    }' | jq . 
```

This will return a JSON payload containing an `authUrl`.

#### **Step 2: Complete Consent in a Browser**

Copy the `authUrl` from the response and paste it into your web browser. You will be directed to the provider's login and consent screen. After you approve, the provider will redirect you back to the `return_url` you specified, which will have a `connection_id` and `status` in the query string.

> **Note:** The `return_url` does not need to be a real, running application for this test. After the redirect, you can simply copy the `connection_id` from the browser's address bar. The final URL will also contain `status` and `provider` as query parameters.

#### **Step 3: Retrieve the Token**

Once you have the `connection_id`, you can use it to retrieve the token from the broker.

```bash
# Replace <connection_id> with the ID from the redirect URL
CONNECTION_ID="<connection_id>"

curl -s -H "X-API-Key: dev-api-key-12345" \
    "http://localhost:8080/connections/''$CONNECTION_ID''/token" | jq . 
```

This will return the access token, refresh token, and expiry information.

---

## 2. Non-OAuth (API Key) Providers

### Registration

Non-OAuth providers are configured by defining a **JSON schema** that describes the credentials the broker needs to collect, AND an **Authentication Strategy** that tells the Bridge client how to use those credentials.

#### **Payload Fields:**

*   `name` (string, required): A unique name for the provider.
*   `auth_type` (string, required): The authentication strategy type.
    *   `"header"`: Inject a value into an HTTP header (e.g., API Keys).
    *   `"query_param"`: Inject a value into the query string.
    *   `"basic_auth"`: Standard HTTP Basic Auth (username/password).
    *   `"aws_sigv4"`: AWS Signature Version 4.
    *   `"hmac_payload"`: HMAC signature of the request body.
    *   `"api_key"`: (Legacy) Alias for `header` type.
*   `params` (json, required): A JSON object containing configuration for both the frontend (schema) and the client (strategy).
    *   `credential_schema` (json, required): A valid JSON schema defining the fields to be collected from the user (e.g., "Enter your API Key").
    *   **Strategy Config**: Any other fields in `params` are treated as configuration for the chosen `auth_type` (e.g., `header_name`, `region`).

### Authentication Strategies & Configuration

The following table shows the required configuration fields in `params` for each strategy.

#### **1. Header Authentication (`header`)**
Injects a value into a specific HTTP header.

**Params Config:**
*   `header_name` (string): The header key (e.g., `X-API-Key`, `Authorization`). Default: `Authorization`.
*   `credential_field` (string): The key from the collected credentials to use. Default: `api_key`.
*   `value_prefix` (string): Optional prefix (e.g., `Bearer `).

#### **2. Query Parameter Authentication (`query_param`)**
Injects a value into the query string.

**Params Config:**
*   `param_name` (string, required): The query param key (e.g., `api_key` for `?api_key=...`).
*   `credential_field` (string): The key from the collected credentials. Default: `api_key`.

#### **3. Basic Authentication (`basic_auth`)**
Uses standard HTTP Basic Auth (base64 encoded user:pass).

**Params Config:**
*   `username_field` (string): Key for the username in credentials. Default: `username`.
*   `password_field` (string): Key for the password in credentials. Default: `password`.

#### **4. AWS Signature V4 (`aws_sigv4`)**
Signs requests using AWS standard signing.

**Params Config:**
*   `service` (string, required): AWS Service (e.g., `s3`, `execute-api`).
*   `region` (string): AWS Region. Default: `us-east-1`.

*Note: The credentials map must contain `access_key` and `secret_key`.*

---

### **Example: Registering a Custom API Provider (Header Auth)**

This provider requires an `api_key` which must be sent in the `X-Freedcamp-Key` header.

```bash
# Define the schema and params as a shell variable
PARAMS='{
  "base_url": "https://freedcamp.com/api/v1/",
  "header_name": "X-Freedcamp-Key",
  "credential_field": "user_key",
  "credential_schema": {
    "type": "object",
    "properties": {
      "user_key": {
        "type": "string",
        "title": "API Key"
      }
    },
    "required": ["user_key"]
  }
}'

# Use jq to construct the final JSON payload
jq -n --argjson params "$PARAMS" '{
    "profile": {
        "name": "freedcamp",
        "auth_type": "header",
        "params": $params
    }
}' | curl -X POST http://localhost:8080/providers \
     -H "Content-Type: application/json" \
     -H "X-API-Key: dev-api-key-12345" \
     -d @- | jq . 
```

### **Example: Registering an AWS Service**

This provider collects AWS credentials and signs requests for API Gateway.

```bash
PARAMS='{
  "service": "execute-api",
  "region": "us-west-2",
  "credential_schema": {
    "type": "object",
    "properties": {
      "access_key": { "type": "string", "title": "AWS Access Key ID" },
      "secret_key": { "type": "string", "title": "AWS Secret Access Key" }
    },
    "required": ["access_key", "secret_key"]
  }
}'

jq -n --argjson params "$PARAMS" '{
    "profile": {
        "name": "my-aws-service",
        "auth_type": "aws_sigv4",
        "params": $params
    }
}' | curl -X POST http://localhost:8080/providers \
     -H "Content-Type: application/json" \
     -H "X-API-Key: dev-api-key-12345" \
     -d @- | jq . 
```

### Testing the Non-OAuth Flow

The flow for non-OAuth providers is entirely API-driven and does not require a browser.

#### **Step 1: Get the Schema Capture URL**

Request a consent spec, just like in the OAuth flow.

```bash
# Replace <provider-id> with the ID returned from registration
PROVIDER_ID="<provider-id>"

# The `authUrl` will be captured into a shell variable
AUTH_URL=$(curl -s -X POST http://localhost:8080/auth/consent-spec \
    -H "Content-Type: application/json" \
    -H "X-API-Key: dev-api-key-12345" \
    -d '{
        "workspace_id": "ws-test-123",
        "provider_id": "'$PROVIDER_ID'",
        "return_url": "http://localhost:3000/my-app-callback"
    }' | jq -r .authUrl)

echo "Schema URL: $AUTH_URL"
```

The `authUrl` returned will point to the broker's `/auth/capture-schema` endpoint.

#### **Step 2: Fetch the JSON Schema**

A client application would call this URL to get the schema needed to render a form.

```bash
# The state parameter is extracted for the next step
STATE=$(echo "$AUTH_URL" | grep -o 'state=[^&]*' | cut -d= -f2)

curl -s -L "$AUTH_URL" | jq . 
```

This returns the provider's name and the `credential_schema` you registered.

#### **Step 3: Submit the Credentials**

Submit the user's credentials along with the `state` from the previous step.

```bash
curl -s -i -X POST http://localhost:8080/auth/capture-credential \
    -H "Content-Type: application/json" \
    -H "X-API-Key: dev-api-key-12345" \
    -d '{
        "state": "'$STATE'",
        "credentials": {
            "user_key": "my-user-supplied-api-key"
        }
    }'
```

This will return a `302 Found` redirect. The `Location` header will contain the `connection_id`.

#### **Step 4: Retrieve the Token**

Extract the `connection_id` from the `Location` header of the previous response and use it to fetch the stored credentials.

```bash
# Replace <connection_id> with the ID from the redirect
CONNECTION_ID="<connection_id>"

curl -s -H "X-API-Key: dev-api-key-12345" \
    "http://localhost:8080/connections/''$CONNECTION_ID''/token" | jq . 
```

The response will now include the strategy:

```json
{
  "strategy": {
    "type": "header",
    "config": {
      "header_name": "X-Freedcamp-Key",
      "credential_field": "user_key"
    }
  },
  "credentials": {
    "user_key": "my-user-supplied-api-key"
  }
}
```

---

## 3. General Provider Management

The following API endpoints can be used to manage any provider, regardless of type. All management endpoints require an API key.

#### **List All Active Providers**

```bash
curl -s http://localhost:8080/providers \
  -H "X-API-Key: dev-api-key-12345" | jq . 
```

#### **Describe a Specific Provider**

Get the full configuration for a single provider (client secret is omitted).

```bash
curl -s http://localhost:8080/providers/<provider-id> \
  -H "X-API-Key: dev-api-key-12345" | jq . 
```

#### **Update a Provider**

Update a provider's configuration. The request body should contain only the fields you want to change.

```bash
curl -s -X PUT http://localhost:8080/providers/<provider-id> \
  -H "Content-Type: application/json" \
  -H "X-API-Key: dev-api-key-12345" \
  -d '{"name": "New Provider Name"}'
```

#### **Delete a Provider**

This performs a "soft delete," marking the provider as inactive but preserving it for existing connections.

```bash
curl -s -X DELETE http://localhost:8080/providers/<provider-id> \
  -H "X-API-Key: dev-api-key-12345"
```

---

## 4. Provider Metadata (Frontend Integration)

To assist frontend applications in rendering dynamic integration lists or performing client-side checks, the broker exposes a metadata endpoint.

#### **Get Grouped Metadata**

Returns a map of all providers, grouped by `auth_type` ("oauth2", "api_key"), containing only the fields necessary for frontend logic (`api_base_url`, `user_info_endpoint`, `scopes`).

```bash
curl -s http://localhost:8080/providers/metadata \
  -H "X-API-Key: dev-api-key-12345" | jq .
```

**Response Example:**
```json
{
  "oauth2": {
    "google": {
      "api_base_url": "https://www.googleapis.com",
      "user_info_endpoint": "/oauth2/v3/userinfo",
      "scopes": ["openid", "email", "profile"]
    },
    "twitter": {
      "api_base_url": "https://api.twitter.com/2",
      "user_info_endpoint": "/users/me",
      "scopes": ["tweet.read", "users.read"]
    }
  },
  "api_key": {
    "freedcamp": {
      "api_base_url": "https://freedcamp.com/api/v1/",
      "user_info_endpoint": "",
      "scopes": null
    }
  }
}
```