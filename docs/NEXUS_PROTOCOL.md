# The Nexus Protocol (v1)

**Status:** Draft / Experimental  
**Version:** 1.0

## Abstract

The **Nexus Protocol** is a standard for **Agent Identity and Connection Orchestration**.

It is designed to decouple the complex **Authentication Mechanics** of external services from the **Application Logic** of autonomous agents.

In a traditional integration, an agent must hardcode the logic for every identity provider it supports (e.g., specific OAuth2 flows, signing logic for AWS, header formats for API Keys). The Nexus Protocol inverts this control: the Agent delegates authentication management to a central **Authority** (The Nexus). The Agent requests a "Connection," and the Nexus returns a **Dynamic Strategy** that instructs the Agent on how to authenticate requests at runtime.

This enables a **"Universal Adapter"** architecture where an Agent can communicate with any upstream service without code changes, driven entirely by server-side configuration.

---

## 1. Core Concepts

*   **Client (Agent)**: The application attempting to communicate with an external service.
*   **Authority (Nexus)**: The central service that manages identity providers, secrets, and token lifecycles.
*   **Connection ID**: An opaque, persistent identifier representing an authorized link between a User and a Provider.
*   **Strategy**: A declarative JSON instruction set telling the Client how to inject credentials into an HTTP/gRPC request.

---

## 2. The Nexus Handshake (Control Plane)

The Handshake is the process of establishing a persistent `Connection ID`.

### Phase 1: Initiation
The Client requests a new connection from the Authority.

1.  **Client** sends `Connection Request`:
    ```json
    {
      "provider_name": "google",
      "scopes": ["email", "profile"],
      "user_id": "workspace-123",
      "return_url": "https://client-app.com/callback"
    }
    ```
2.  **Authority** responds with:
    *   `auth_url`: The URL to redirect the user to (hosted by the Authority or Provider).
    *   `connection_id`: The pending identifier for this session.

### Phase 2: Consent
1.  The User is redirected to `auth_url`.
2.  The Authority manages the specific interactions required (e.g., OAuth2 redirect chain, API Key form capture).
3.  Upon success, the Authority marks the `connection_id` as `ACTIVE`.

### Phase 3: Activation
1.  The Authority redirects the User back to the Client's `return_url` with the `connection_id` and status.
2.  (Optional) The Client may also Poll the Authority to check when the `connection_id` becomes `ACTIVE`.

### 2.1. Connection States
The Protocol defines a standard state machine for connections.

| State | Definition |
| :--- | :--- |
| **PENDING** | Initial state. Handshake initiated, awaiting user consent. |
| **ACTIVE** | Healthy. Credentials are valid, token refreshes are succeeding. |
| **REVOKED** | Terminated. Explicitly disabled by Admin/User. Client receives `401`. |
| **EXPIRED** | Dead. Credentials expired and cannot be refreshed (e.g., password changed). Requires re-consent. |
| **FAILED** | Error. Setup failed during consent or initial exchange. |

---

## 3. The Nexus Interface (Data Plane)

Once a connection is established, the Client retrieves **Strategies**, not just tokens. This is the core innovation of the Nexus Protocol.

### The Credential Request
The Client requests valid credentials for a specific connection.

*   **Request:** `GET /token/{connection_id}`
*   **Response:**
    ```json
    {
      "strategy": {
        "type": "<STRATEGY_TYPE>",
        "config": { ... }
      },
      "credentials": {
        "access_token": "...",
        "secret_key": "..."
      },
      "expires_at": 1735689600
    }
    ```

### Supported Strategies

The Client MUST implement interpreters for the following strategies.

#### 1. Header Injection (`header`)
Injects a static or dynamic value into a specific HTTP header.

*   **Config Schema:**
    *   `header_name` (string): The header key (e.g., `Authorization`, `X-API-Key`).
    *   `credential_field` (string): The key in the `credentials` map to use as the value.
    *   `value_prefix` (string, optional): A prefix to prepend (e.g., `Bearer `).

*   **Example Payload:**
    ```json
    {
      "strategy": {
        "type": "header",
        "config": { "header_name": "Authorization", "value_prefix": "Bearer ", "credential_field": "token" }
      },
      "credentials": { "token": "ey..." }
    }
    ```

#### 2. Query Parameter (`query_param`)
Injects a value into the request's query string.

*   **Config Schema:**
    *   `param_name` (string): The query parameter key (e.g., `api_key`).
    *   `credential_field` (string): The key in the `credentials` map to use.

#### 3. Basic Authentication (`basic_auth`)
Applies standard HTTP Basic Auth (base64 encoded `user:pass`).

*   **Config Schema:**
    *   `username_field` (string): Key for the username.
    *   `password_field` (string): Key for the password.

#### 4. AWS Signature V4 (`aws_sigv4`)
Signs the request using the AWS SigV4 standard.

*   **Config Schema:**
    *   `region` (string): Target AWS region.
    *   `service` (string): Target AWS service (e.g., `execute-api`).
*   **Requirements:** The `credentials` map MUST contain `access_key` and `secret_key`.

#### 5. OAuth 2.0 (`oauth2`)
A specialized alias for `header` injection, maintained for semantic clarity.

*   **Implicit Config:**
    *   `header_name`: `Authorization`
    *   `value_prefix`: `Bearer `
    *   `credential_field`: `access_token`

---

## 4. The Agent Lifecycle

The Protocol defines a standard behavior loop for Agents to ensure resilient communication. This formalizes the role of a "Bridge" library.

### 4.1. Resolution
The Agent initiates a session by resolving the `connection_id` against the Authority.
*   **Action:** Call `GET /token/{connection_id}`.
*   **Outcome:** Receive `Strategy` and `Credentials`.

### 4.2. Configuration
The Agent interprets the `Strategy` to configure its transport layer.
*   **Stateless Transports (HTTP):** The strategy is applied to every request (e.g., injecting headers).
*   **Stateful Transports (WebSocket/gRPC):** The strategy is applied during the handshake or connection dial.

### 4.3. Maintenance & Rotation
The Agent MUST monitor the validity of its credentials.

1.  **Expiry Monitoring:** If `expires_at` is provided, the Agent MUST preemptively refresh credentials before they expire.
2.  **Error Handling:** If the External Service returns a `401 Unauthorized` (or equivalent), the Agent MUST:
    *   Pause communication.
    *   Force a refresh from the Authority (`POST /refresh`).
    *   Re-apply the new credentials.
    *   Resume communication.
3.  **Backoff:** If the Authority is unreachable, the Agent MUST apply exponential backoff to avoid thundering herd scenarios.

---

## 5. Implementation Reference

The **Nexus Framework** is the reference implementation of the Nexus Protocol.

| Protocol Concept | Nexus Implementation |
| :--- | :--- |
| **Authority** | `nexus-broker` + `nexus-gateway` |
| **Agent (Client)** | `bridge` (Go Library) |
| **Handshake** | `POST /v1/request-connection` |
| **Strategy Engine** | `bridge/internal/auth` package |

---

## 6. Security Considerations

1.  **Opaque Transport:** The Client treats the `credentials` map as opaque data necessary to fulfill the `strategy`. It should not attempt to parse tokens unless explicitly required by the strategy.
2.  **Centralized Secrets:** Secrets are stored only by the Authority. The Client fetches them on-demand and holds them in memory only for the duration of the request or a short TTL.
