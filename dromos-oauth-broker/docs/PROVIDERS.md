# Provider Registration and Management Guide

This guide provides a comprehensive overview of how to register, manage, and test identity providers within the Dromos OAuth Broker.

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
*   `issuer` (string, required): The OIDC issuer URL. The broker will use this to discover the authorization and token endpoints.
*   `client_id` (string, required): The OAuth client ID from the provider.
*   `client_secret` (string, required): The OAuth client secret from the provider.
*   `scopes` (string array, required): A list of default scopes to request.
*   `auth_url` (string, optional): Override for the authorization endpoint.
*   `token_url` (string, optional): Override for the token endpoint.
*   `params` (json, optional): A JSON object for provider-specific parameters (e.g., `{"access_type": "offline"}`).

#### **Example: Registering Google via OIDC Discovery**

```bash
curl -X POST http://localhost:8080/providers \
     -H "Content-Type: application/json" \
     -H "X-API-Key: dev-api-key-12345" \
     -d 
{
        "name": "google",
        "issuer": "https://accounts.google.com",
        "client_id": "YOUR_GOOGLE_CLIENT_ID",
        "client_secret": "YOUR_GOOGLE_CLIENT_SECRET",
        "scopes": ["openid", "email", "profile"],
        "params": {
            "access_type": "offline",
            "prompt": "consent"
        }
     }
 | jq . 
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
    -d 
{
        "workspace_id": "ws-test-123",
        "provider_id": "'$PROVIDER_ID'",
        "scopes": ["openid", "email"],
        "return_url": "http://localhost:3000/my-app-callback"
    }
 | jq . 
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

Non-OAuth providers are configured by defining a **JSON schema** that describes the credentials the broker needs to collect. This allows for maximum flexibility, as the broker does not need to know the specific fields in advance.

#### **Payload Fields:**

*   `name` (string, required): A unique name for the provider.
*   `auth_type` (string, required): Must be set to `"api_key"`.
*   `params` (json, required): A JSON object containing a `credential_schema`.
    *   `credential_schema` (json, required): A valid JSON schema defining the fields to be collected.

#### **Example: Registering a Custom API Provider**

This provider requires an `api_key` and an `api_secret`.

```bash
# Define the schema as a shell variable
SCHEMA='{
  "type": "object",
  "properties": {
    "api_key": {
      "type": "string",
      "title": "API Key"
    },
    "api_secret": {
      "type": "string",
      "title": "API Secret"
    }
  },
  "required": ["api_key", "api_secret"]
}'

# Use jq to construct the final JSON payload
jq -n --argjson schema "$SCHEMA" 
{
    "name": "freedcamp",
    "auth_type": "api_key",
    "params": {
      "base_url": "https://freedcamp.com/api/v1/",
      "credential_schema": $schema
    }
}
 | curl -X POST http://localhost:8080/providers \
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
    -d 
{
        "workspace_id": "ws-test-123",
        "provider_id": "'$PROVIDER_ID'",
        "return_url": "http://localhost:3000/my-app-callback"
    }
 | jq -r .authUrl)

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
    -d 
{
        "state": "'$STATE'",
        "credentials": {
            "api_key": "my-user-supplied-api-key",
            "api_secret": "my-user-supplied-secret"
        }
    }
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

This will return the credentials you submitted in Step 3, now securely stored and encrypted by the broker.

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