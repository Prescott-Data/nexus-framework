# Technical Debt & Design Log for the Bridge Library

This document outlines outstanding work, design decisions, and requirements for the `bridge` and its surrounding ecosystem.

## 1. Observability: Agent-Side Requirements (CRITICAL)

The `telemetry` package and `NewStandard` constructor provide out-of-the-box Prometheus metrics. However, to make this data useful in a dynamic environment like Kubernetes, one final step is required.

### Action Item: Add Per-Agent Labels to Metrics

*   **Task:** The `telemetry/metrics.go` `NewMetrics` function should be updated to accept a map of `agent_labels` (e.g., `{"agent_id": "my-stable-id"}`). These labels **must** be applied as `const_labels` to all Prometheus metrics registered by the Bridge.
*   **Rationale:** Without a stable, unique identifier for each agent instance, it is impossible to track an individual agent's health, perform accurate counts of running agents, or debug reconnnection loops for a specific agent. This is the final step to make the telemetry data actionable for production monitoring.
*   **Status:** **TODO**. This is a high-priority item.

---

## 2. Backend Dependency: Generic Provider Support in Broker

This section outlines the critical backend work required in the `dromos-oauth-broker` service to enable the Bridge's new multi-protocol authentication capabilities.

### Status: **BLOCKING**

The `bridge` client has been upgraded to a "Universal Connector," capable of handling various authentication strategies (`basic_auth`, `hmac`, `aws_sigv4`, etc.). However, this functionality is **currently disabled** because the Broker is not yet capable of storing or serving these new configurations.

### Action Items for `dromos-oauth-broker`:

1.  **Database Schema Modifications:**
    *   **Task:** Create a new database migration. The `provider_configurations` table (or equivalent) needs columns to store the generic authentication strategy.
    *   **Required Fields:**
        *   `auth_strategy_type` (string, e.g., "basic_auth", "aws_sigv4")
        *   `auth_strategy_config` (jsonb, e.g., `{"header_name": "X-API-Key", "region": "us-east-1"}`)
    *   **Note:** While some migrations like `04_refactor_providers_for_non_oauth.sql` exist, a new, comprehensive migration is likely needed to store the structured `strategy` and `config` JSON that the Bridge now expects.

2.  **Update Broker API Endpoint (`/connections/{id}/token`):**
    *   **Task:** The handler for this endpoint must be updated to serve the new generic credential payload.
    *   **Logic:**
        1.  Fetch the provider configuration for the given `connection_id`.
        2.  If it's a generic provider, construct the JSON payload:
            ```json
            {
              "strategy": {
                "type": "<auth_strategy_type from DB>",
                "config": <auth_strategy_config from DB>
              },
              "credentials": <the decrypted credentials map>
            }
            ```
        3.  If it's a traditional OAuth2 provider, construct the backward-compatible payload:
            ```json
            {
              "strategy": { "type": "oauth2" },
              "credentials": { "access_token": "...", "expires_at": ... },
              "access_token": "...",
              "expires_at": ...
            }
            ```
    *   **Rationale:** Without this change, the Broker will continue to send only OAuth2-style responses, and the Bridge's new authentication engine will never be used for other strategies.

### Rationale:

The Bridge is the "engine," but the Broker is the "fuel tank." Until the Broker is upgraded to store and provide the correct "fuel" (the generic auth configurations), the engine can only run on its old "OAuth2" fuel. This backend work is the final step to unlock the full potential of the universal connector.

---

## 3. Design Decisions & Trade-offs

### `RequireTransportSecurity` in gRPC Credentials

*   **Decision:** The `BridgeCredentials.RequireTransportSecurity()` method defaults to `false`.
*   **Reasoning:** This prioritizes ease of use for local testing and internal services on trusted networks.
*   **Trade-off:** Security can still be enforced by the user by providing `grpc.WithTransportCredentials(...)` in the dial options. This was deemed an acceptable trade-off versus forcing users to disable a "secure-by-default" setting for common development scenarios.