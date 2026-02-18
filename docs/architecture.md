# Architecture Overview

The Nexus Framework is split into a **Control Plane** (managing connections) and a **Data Plane** (using connections).

## System Components

### 1. Nexus Broker (The Authority)
The Broker is the source of truth and the most sensitive component.
- **Responsibilities:**
    - Stores Provider profiles (OAuth configs, API Key schemas).
    - Performs the OAuth 2.0 / OIDC handshake logic.
    - Manages the **Encryption Vault**: Tokens are encrypted at rest using AES-GCM 256-bit with a Master `ENCRYPTION_KEY`.
    - Runs the background **Refresh Loop**: Preemptively refreshes tokens before they expire.
- **Technology Stack:** Go, PostgreSQL (Persistence), Redis (Caching/Discovery).

### 2. Nexus Gateway (The Proxy)
The Gateway is the public-facing entry point for Agents.
- **Responsibilities:**
    - Provides a unified REST and gRPC API.
    - Proxies requests to the Broker using an internal `BROKER_API_KEY`.
    - Decouples Agents from the internal Broker infrastructure.
    - Handles CORS, request validation, and API versioning.
- **Technology Stack:** Go, gRPC, gRPC-Gateway.

### 3. Nexus Bridge (The Library)
A Go library that runs inside the Agent's process.
- **Responsibilities:**
    - Automatically retrieves the latest **Strategy** and **Credentials** from the Gateway.
    - Injects authentication headers into outgoing HTTP and gRPC requests.
    - Manages persistent WebSocket and gRPC connections.
    - Implements retries and exponential backoff.

### 4. Nexus SDK (The Client)
A thin, lightweight client used by the Agent to initiate handshakes (e.g., `RequestConnection`).

## Data Flow: The Handshake

1.  **Initiation:** Agent calls Gateway `POST /v1/request-connection`.
2.  **Spec Generation:** Broker generates a unique `state` and a temporary `connection_id`.
3.  **Redirection:** Agent redirects User to the `auth_url` returned by the Gateway.
4.  **Consent:** User authorizes the connection on the Provider's site.
5.  **Capture:** Broker receives the callback, exchanges the code for tokens, encrypts them, and stores them in PostgreSQL.
6.  **Activation:** The connection status transitions to `ACTIVE`.

## Data Flow: Usage (Signing)

1.  **Retrieval:** Bridge calls Gateway `GET /v1/token/{id}`.
2.  **Decryption:** Broker retrieves tokens, decrypts them in RAM, and returns them to the Gateway.
3.  **Delivery:** Gateway returns a **Strategy** (e.g., "Inject into 'Authorization' header") and the **Credentials**.
4.  **Signing:** Bridge interprets the strategy and modifies the Agent's request headers before sending it to the Provider.
