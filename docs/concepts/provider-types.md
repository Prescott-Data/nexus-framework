---
icon: material/puzzle-outline
---

# Provider Types

A provider profile tells Nexus how to authenticate users against a third-party service. The provider type determines the authorization flow and the shape of stored credentials.

## OAuth2

OAuth2 providers use the Authorization Code flow with PKCE. Nexus manages the full token lifecycle, so agents can receive a current access token.

### OIDC discovery

Set `enable_discovery: true` and provide an `issuer` URL. Nexus fetches `{issuer}/.well-known/openid-configuration` to populate `authorization_endpoint` and `token_endpoint` automatically.

Use this for Google, Microsoft Entra ID, Auth0, and any provider with a published discovery document.

### Manual configuration

Set `auth_url` and `token_url` explicitly. Use this for GitHub and other OAuth2 providers without a discovery document.

### Provider profile fields

| Field | Required | Description |
|---|---|---|
| `name` | yes | Unique name for this provider within your Nexus instance |
| `auth_type` | yes | `oauth2`, `saml`, `api_key`, `basic_auth`, `header`, `query_param`, `hmac_payload`, or `aws_sigv4` |
| `client_id` | OAuth2 | OAuth2 application client ID |
| `client_secret` | OAuth2 | OAuth2 application client secret |
| `auth_url` | OAuth2 (manual) | Authorization endpoint |
| `token_url` | OAuth2 (manual) | Token endpoint |
| `issuer` | OAuth2 (discovery) | OIDC issuer URL |
| `enable_discovery` | no | `true` to use OIDC discovery |
| `scopes` | no | Default OAuth2 scopes for this provider |
| `saml_idp_entity_id` | SAML | Identity Provider entity ID |
| `saml_idp_sso_url` | SAML | Identity Provider SSO endpoint |
| `saml_idp_x509_cert` | SAML | IdP signing certificate as PEM or base64 DER |
| `saml_sp_entity_id` | SAML | Nexus Service Provider entity ID |
| `api_base_url` | static | Root URL used to validate the credential, for example `https://api.example.com` |
| `user_info_endpoint` | static | Path appended to `api_base_url` to validate the credential, for example `/me` |
| `auth_header` | no | Header name for static-key injection, default `Authorization` |
| `params` | no | Additional provider-specific parameters as JSON |

### PKCE

All OAuth2 flows use PKCE (RFC 7636). The Broker generates a random `code_verifier`, sends the SHA-256 `code_challenge` to the provider, and verifies the exchange on callback. You do not configure this; it is always enabled.

## SAML 2.0

SAML providers let Nexus act as a Service Provider (SP) for enterprise Identity Providers such as Okta, Microsoft Entra ID, ADFS, Ping, and Shibboleth.

Register a provider with `auth_type: "saml"` and the IdP metadata fields listed above. `saml_idp_x509_cert` is used to validate signed SAML responses. `saml_sp_entity_id` must match the entity ID configured in the IdP application.

The connection flow uses the existing consent endpoint:

1. Call `POST /v1/request-connection` through the Gateway, or `POST /auth/consent-spec` directly against the Broker.
2. Nexus creates a pending connection, generates a SAML AuthnRequest, signs RelayState, and returns the IdP redirect URL as `authUrl`.
3. The user's browser completes authentication at the IdP.
4. The IdP posts `SAMLResponse` and `RelayState` to the Broker's public ACS endpoint: `/saml/acs`.
5. The Broker validates the response signature, checks assertion conditions, encrypts the extracted `name_id` and attributes, activates the connection, and redirects to the original `return_url`.

Administrators can retrieve SP metadata from `GET /saml/metadata/{providerID}` on the Broker with `X-API-Key`. Upload that XML to the IdP when the IdP supports metadata import.

SAML assertions cannot be refreshed. When the stored assertion expires, Nexus marks the connection `attention` and the user must authenticate again.

SAML is an identity assertion flow, not a generic HTTP credential injection strategy. Token retrieval returns `strategy.type: "saml"` with assertion attributes for applications that need identity context. The Bridge does not apply SAML assertions to arbitrary downstream APIs.

## Static credentials

Static providers authenticate with credentials that do not expire and cannot be refreshed.

### api_key

A single opaque key. Your backend calls `GET /v1/capture-schema` to get the field definition, presents it to the user, and submits via `POST /v1/capture-credential`. Set `auth_strategy` to `header` or `query_param` on the provider profile to control how the key is injected.

Before the connection is activated, the Broker validates the credential against the provider (see [Credential validation](#credential-validation)). Only if validation passes does the connection move to `active`.

### basic_auth

Username and password pair. The capture flow is identical to `api_key`, including credential validation. The stored credentials map has `username` and `password` keys. The auth strategy is always `basic_auth`.

### Credential validation

For `api_key` and `basic_auth`, the Broker verifies the submitted credential before storing it. It sends a `GET` request to `{api_base_url}{user_info_endpoint}` with the credential applied, and:

- `2xx` or non-auth error: credential accepted, connection set to `active`.
- `401` or `403`: credential rejected with `invalid_credentials`; the connection is not activated.

Because of this, a static provider must be registered with both `api_base_url` and `user_info_endpoint`. If either is missing, capture fails closed with `provider_not_validatable` rather than activating an unverified credential. Pick a `user_info_endpoint` that returns `401` or `403` for a bad key, such as `/me`, `/user`, or `/v1/account`.

The validator injects the credential as an HTTP header (`Authorization: Bearer <key>` by default, or the header named by `auth_header`). Providers that require the key elsewhere, for example in the URL path (`/bot<token>/...`), cannot be validated by the current validator and will fail closed until path-based validation is supported.

## Scopes

The `scopes` array on the provider profile is the default for new OAuth2 connections. Individual OAuth2 connections can request a different subset by passing `scopes` to `POST /v1/request-connection`. Static and SAML providers ignore scopes.

## Registration and deletion

Register providers via `POST /v1/providers`. Each provider has a unique name. Deleting a provider with `DELETE /v1/providers/{id}` does not delete its connections; clean up connections first to avoid orphaned records.
