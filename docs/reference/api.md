# API Reference

The Nexus Framework exposes two primary APIs: the public-facing **Gateway API** for agents and the internal **Broker API** for provider management. Both services use OpenAPI 3.0 specifications.

## 1. Gateway API (Public)
The Gateway is the single entry point for agents and services to interact with the Nexus ecosystem. It provides a stable, versioned contract for requesting connections and retrieving credentials.

- **Spec File:** [`openapi.yaml`](https://raw.githubusercontent.com/Prescott-Data/nexus-framework/main/openapi.yaml)
- **Base URL:** `https://gateway.example.com` (Production) / `http://localhost:8090` (Local)
- **Client SDK:** [`nexus-sdk`](https://github.com/Prescott-Data/nexus-framework/tree/main/nexus-sdk) (Go)

### Key Endpoints

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/v1/request-connection` | Initiates an OAuth or credential capture flow. Returns an auth URL. |
| `GET` | `/v1/check-connection/{id}` | Checks the status of a connection (`active`, `pending`, `failed`). |
| `GET` | `/v1/token/{id}` | Retrieves active credentials. Automatically handles token refresh if needed. |
| `POST` | `/v1/refresh/{id}` | Forces a token refresh with the upstream provider. |

## 2. Broker API (Internal)
The Broker is the internal engine that manages provider configurations, handles OAuth callbacks, and stores encrypted tokens. It should **not** be exposed to the public internet (except for the callback endpoints).

- **Spec File:** [`nexus-broker/openapi.yaml`](https://raw.githubusercontent.com/Prescott-Data/nexus-framework/main/nexus-broker/openapi.yaml)
- **Base URL:** `http://localhost:8080` (Local)

### Provider Management

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/providers` | Register a new provider profile. |
| `GET` | `/providers` | List all registered providers. |
| `PUT` | `/providers/{id}` | Update a provider configuration. |
| `DELETE` | `/providers/{id}` | Soft-delete a provider. |

### Auth Flow & Callbacks

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/auth/consent-spec` | Generates the state and URL for a new auth flow. |
| `GET` | `/auth/callback` | **Public.** Handles OAuth 2.0 redirects from providers. |
| `GET` | `/auth/capture-schema` | **Public.** Serves the JSON schema for non-OAuth credential capture. |
| `POST` | `/auth/capture-credential` | **Public.** Accepts credentials for non-OAuth providers. |

## Authentication

- **Gateway:** Public endpoints are open. Protected endpoints (if any in future versions) use standard API keys or tokens.
- **Broker:** All management endpoints require an internal API Key sent in the `X-API-Key` header.
