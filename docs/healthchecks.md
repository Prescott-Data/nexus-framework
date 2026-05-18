# Health Checks Architecture

## Overview
To ensure the reliability of the Nexus Framework, we need a systemic, automated way to monitor integration health. Since the `nexus-broker` acts as the central directory and credential manager, it is the ideal component to orchestrate and record these health checks.

True reliability requires monitoring health across two distinct dimensions: **Provider-Level** (Global) and **Connection-Level** (User-Specific). 

This document outlines the proposed architecture for a robust, two-tiered health check system.

---

## 1. The Two Dimensions of Health

A health check that simply pings an API endpoint and returns `200 OK` is insufficient. If a user's specific API key was revoked, the integration is broken for them, regardless of whether the external server is online. Therefore, we distinguish between two types of health:

### A. Provider-Level Health (Global)
*   **Target Audience:** Platform Administrators.
*   **The Question:** "Is Google/Stripe currently experiencing a system-wide outage?"
*   **The Goal:** Detect systemic outages or misconfigurations in the global Provider Profile (e.g., the global OAuth `client_secret` rotated). 
*   **The Action:** Trigger alerts to the Ops team, update status pages, and optionally suspend new connection attempts to prevent user frustration during a known outage.

### B. Connection-Level Health (User-Specific)
*   **Target Audience:** End-Users and acting Agents.
*   **The Question:** "Is this specific user's API key or OAuth Refresh Token still valid?"
*   **The Goal:** Detect credential rot (e.g., user changed their password, API key expired, OAuth token revoked, or service account deleted).
*   **The Action:** Instantly mark the connection as `expired` or `revoked` and proactively prompt the user to re-authenticate via the frontend UI, rather than letting agents fail mysteriously in the background.

When these two systems work in tandem, they provide perfect observability. For example, if a Connection check fails but the Provider check is healthy, it is an isolated user error. If both fail, it is a systemic upstream outage.

---

## 2. Provider-Level Health Checks (Active Probing)

An automated background worker (the "Heartbeat" Worker) inside the `nexus-broker` actively checks all registered providers on a set interval (e.g., every 5 minutes).

For each provider, it performs a **Tiered Health Check**:

#### Tier 1: Endpoint Reachability (Shallow Check)
*   **Action:** If the provider supports OIDC, fetch the `.well-known/openid-configuration` document. If not, make an HTTP `HEAD` or `OPTIONS` request to the provider's `auth_url` and `token_url`.
*   **Result:** Proves that DNS resolves correctly and the provider's servers are online and accepting connections.

#### Tier 2: Configuration Validation (Deep Check - Implemented in Phase 1)
*   **Action:** For OAuth2 providers, send a simulated authentication request to the `token_url` using the provider's configured `client_id` and an intentionally invalid code/secret.
*   **Expected Result:** The provider should respond with a fast `400 Bad Request` or `401 Unauthorized` containing a standard OAuth error payload (e.g., `invalid_grant`).
*   **Why this works:** If the check results in a timeout, a `500 Server Error`, or an HTML error page, we know the provider's API is down. Receiving the specific OAuth error confirms the API is alive and actively processing OAuth requests correctly.

#### Tier 3: Synthetic User Refresh (Full End-to-End Check)
*   **Action:** For business-critical providers (especially non-OAuth2 providers like API Keys), we maintain a "Synthetic Connection" in the database (a real, authorized connection belonging to a test bot). The worker attempts to use the `refresh_token` or test `api_key` for this connection.
*   **Result:** Proves that the entire pipeline—network, credentials, scopes, and the provider's token issuance engine—is 100% operational.

---

## 3. Data Architecture (Provider-Level)

To support the Provider-Level system, the Broker's database schema utilizes the following:

### Database Schema Updates
New health-tracking fields added to the `provider_profiles` table:
*   `last_health_check_at` (TIMESTAMP)
*   `health_status` (ENUM: `healthy`, `degraded`, `unhealthy`, `unknown`)
*   `health_message` (TEXT, e.g., "Timeout reaching token_endpoint")

### API Endpoint Additions
A new API endpoint to expose health data:
*   **`GET /providers/health`**
    *   Returns a dashboard-friendly JSON payload detailing the current status of all providers.
    *   Useful for integrating with external monitoring systems (Datadog, Grafana, OpsGenie, etc.).

---

## 4. Implementation Plan

### Phase 1: Provider-Level Deep Checks (Completed)
1.  **Worker Initialization:** Added a `HealthWorker` to `cmd/nexus-broker/main.go` running on a recurring 5-minute ticker.
2.  **Tier 2 Implementation:** Configured the worker to query all active OAuth2 providers, perform a **Tier 2 Deep Check** against their respective `token_url` endpoints, and parse the responses.
3.  **State Management:** Updated the `provider_profiles` table with the result of the check (`health_status`, `last_health_check_at`, `health_message`).
4.  **Observability:** Exposed the `GET /providers/health` endpoint for monitoring and alerting integrations.

### Phase 2: Connection-Level Checks (Planned)
1.  **Connection Verifier Worker:** Build a new worker that iterates through active `connections`.
2.  **Credential Validation:** For `api_key` or `basic_auth` connections, periodically decrypt the credential and make a lightweight, read-only request (e.g., `GET /v1/users/me`).
3.  **State Management:** If the request returns `401 Unauthorized`, automatically flip the connection status to `expired`.
4.  **User Experience:** Frontend clients query `GET /connections` and prompt users to re-authenticate if their specific connection is marked expired.