# Technical Debt & Infrastructure Requirements for Bridge Monitoring

This document outlines the necessary work to enable robust, end-to-end monitoring of agents using the `bridge` library. This requires coordination between the agent development team and the infrastructure/gateway team.

## The Goal

To gain a complete view of agent connectivity health by capturing metrics from both the client-side (agent/bridge) and server-side (gateway) and correlating them in a central monitoring system (Prometheus).

## 1. Agent-Side Requirements

This work needs to be implemented by the developers of the agent application that consumes the `bridge` library.

### Action Items:

1.  **Implement a Stable Agent Identity:**
    *   The agent application **must** generate a unique and persistent ID for itself. This ID must survive process restarts.
    *   **Good ID Strategies:** A UUID generated on first launch and saved to a local file, the machine's hostname, or another stable identifier provided by the deployment environment.
    *   This stable ID **must** be passed as the `connectionID` parameter to the `bridge.MaintainWebSocket()` function.

2.  **Implement and Export Prometheus Metrics:**
    *   The agent must implement the `bridge.Metrics` interface.
    *   This implementation should use a Prometheus client library (e.g., `prometheus/client_golang`).
    *   All exported metrics **must** include the stable agent ID as a label (e.g., `bridge_connection_status{agent_id="<your-stable-id>"}`).
    *   The agent must expose a `/metrics` endpoint for Prometheus to scrape.

### Rationale:

The bridge library provides the hooks (`Metrics` interface, `connectionID` parameter), but the agent application is responsible for using them. Without a stable `agent_id` label, it is impossible to track an individual agent's health and history across ephemeral connections and restarts.

## 2. Infrastructure/Gateway Requirements

This work needs to be implemented by the team managing the backend infrastructure and the `oauth-gateway`.

### Action Items:

1.  **Implement Gateway Metrics:**
    *   The `oauth-gateway` should be instrumented to collect and expose its own set of critical metrics (e.g., active connections, request/error rates, latency histograms).
    *   These metrics should also be exported in a Prometheus-compatible format via a `/metrics` endpoint.

2.  **Configure Prometheus Scraping:**
    *   The Prometheus server must be configured to discover and scrape the `/metrics` endpoints from both the running agents and the `oauth-gateway` instances.

### Rationale:

Server-side metrics provide the ground truth for server load and overall connection counts. Client-side metrics provide the crucial context of the client's experience (reconnect loops, latency, etc.). Both are required for a complete picture of system health.

---

## 3. Design Decisions & Trade-offs

### `RequireTransportSecurity` in gRPC Credentials

*   **Decision:** The `BridgeCredentials.RequireTransportSecurity()` method defaults to `false`.
*   **Reasoning:** When this method returns `true`, the gRPC client will fail to connect if the user provides `grpc.WithTransportCredentials(insecure.NewCredentials())`. This makes local testing and connecting to internal services on a trusted network difficult. By defaulting to `false`, we prioritize ease of use for the most common development scenarios.
*   **Trade-off:** This is a "secure-by-default" vs. "easy-by-default" choice. We chose the latter because security is still easily enforced by the user. An application connecting to a secure production endpoint **must** provide `grpc.WithTransportCredentials(credentials.NewTLS(...))` in the dial options, which will correctly establish a secure connection regardless of this setting.
*   **Future Work:** We could add a `WithSecurity(bool)` option to `NewBridgeCredentials` to allow users to explicitly override this default if their internal policies require it.