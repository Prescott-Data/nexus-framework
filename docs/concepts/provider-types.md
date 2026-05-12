# Provider Types

A provider in Nexus represents a third-party service that your agents need to authenticate against. Nexus supports two categories of provider: OAuth 2.0 providers and static key providers. The category determines how Nexus acquires credentials, how it stores them, and what it returns when an agent requests a session.

---

## OAuth 2.0 providers

An OAuth 2.0 provider uses the standard authorization code flow: a user grants consent, the provider issues tokens, and Nexus stores and manages those tokens on the user's behalf.

Nexus supports two configuration approaches for OAuth 2.0 providers.

Discovery-based providers expose an OIDC discovery endpoint at `/.well-known/openid-configuration`. You supply the issuer URL, and Nexus fetches the authorization endpoint, token endpoint, and JWKS URI automatically. Google, Microsoft Entra, Okta, and most modern identity platforms support discovery.

Manual configuration is for providers that do not support OIDC discovery. You supply the authorization URL and token URL explicitly. GitHub, Twitter, and many API-first products fall into this category.

Both approaches result in the same runtime behavior: Nexus holds the refresh token, runs the background refresh loop, and returns a short-lived access token when an agent requests credentials.

---

## Static key providers

A static key provider does not use OAuth. Instead, you register a JSON schema that describes the shape of the credential (an API key, a username and password, an AWS access key pair, and so on). When a connection is established, the user or system supplies values matching that schema. Nexus encrypts and stores the values. When an agent requests credentials, Nexus decrypts them and returns them as a structured payload.

Static key providers do not have a background refresh loop because the credentials do not expire on a schedule managed by Nexus. If a static credential is rotated externally, you update the connection in Nexus manually.

---

## The credential payload

Regardless of provider type, the credential retrieval endpoint returns a consistent structure:

```json
{
  "strategy": { "type": "oauth2" },
  "credentials": {
    "access_token": "eyJ...",
    "expires_at": 1715000000
  }
}
```

For a static key provider, the `strategy.type` is `api_key` or `basic_auth`, and the `credentials` object contains the fields defined by the provider's schema.

Your agent inspects `strategy.type` to determine how to use the credentials. The Bridge handles this automatically. If you are making direct HTTP calls, you apply the credentials based on the strategy type.

---

## Provider aliases

Every provider is assigned a human-readable name (its alias) at registration time. Aliases are the identifier you use throughout Nexus: in connection requests, in the `nexus-providers.yaml` manifest, and in audit log entries. Internally, each provider also has a UUID, but you never need to use it directly.

Aliases must be unique within a workspace. Choose names that are stable and descriptive: `google-workspace`, `github-ci`, `salesforce-prod`. Renaming an alias requires updating every connection that references it.
