# Dromos: A Lightweight, Schema-Driven Broker for Unified Credential Management

## Abstract

In modern distributed systems, applications require access to a multitude of third-party services, each with its own authentication mechanism. This proliferation of credentials—from OAuth 2.0 tokens to static API keys—creates significant complexity, security risks, and development overhead. This paper introduces the Dromos OAuth Framework, a suite of services designed to abstract and unify credential management. The framework is composed of the Dromos OAuth **Broker**, a secure credential vault; the Dromos **Gateway**, a stable API front door; and the **Bridge**, a universal client library for persistent connections. By providing a single, secure ecosystem for acquiring, storing, and using credentials, Dromos decouples client applications from the intricacies of external authentication, enforcing consistent security policies and drastically reducing development friction.

---

## 1. Introduction: The Challenge of Distributed Credential Management

The shift towards microservice architectures has led to an explosion in the number of service-to-service integrations. A single business process may require an application to interact with numerous external APIs for data, payments, or identity. This presents a significant challenge:

*   **Authentication Heterogeneity**: Each external service may use a different authentication standard. An application might need to handle OAuth 2.0 for Google, OIDC for Okta, and a variety of bespoke API key or bearer token schemes for other SaaS platforms.
*   **Increased Development Overhead**: Implementing and maintaining each of these authentication flows is a repetitive and error-prone task. It forces application developers to become security experts, diverting focus from core business logic.
*   **Decentralized Security Risk**: When each microservice manages its own credentials, the attack surface of the system grows exponentially. Storing secrets, implementing token refresh logic, and handling OAuth callbacks securely within each service is a recipe for inconsistent security and potential vulnerabilities.

This decentralized model is inefficient, insecure, and unscalable. It creates a need for a centralized solution that can act as a trusted intermediary for all external authentication.

---

## 2. The Dromos OAuth Framework: A Unified Ecosystem

The Dromos OAuth Framework is a collection of services and libraries that provide a complete, end-to-end solution for credential management.

*   **The Dromos OAuth Broker**: The stateful heart of the system. It is a secure, private service responsible for all sensitive operations, including OAuth 2.0 flows, credential validation, encryption, and storage.
*   **The Dromos Gateway**: A stateless, public-facing API gateway. It provides a stable, versioned API for client applications, proxying requests to the Broker and insulating clients from internal architectural changes.
*   **The Bridge Client Library**: A universal Go client for agents that require persistent, authenticated connections. It integrates seamlessly with the Gateway to handle both **WebSocket** and **gRPC** transports, abstracting away all authentication and reconnection logic.

---

## 3. System Architecture

Dromos is designed as a set of minimal, high-performance Go services with a PostgreSQL backend for the Broker.

### 3.1. The Dromos Gateway (The Front Door)

The Gateway acts as the primary entry point for all client applications. As a stateless proxy, its role is to provide a clean, secure, and stable API contract.

*   **API Stability**: The Gateway exposes a versioned API (e.g., `/v1/...`). This allows the internal Broker API to evolve without breaking existing client integrations.
*   **Request Validation & Routing**: It performs initial validation of requests and routes them to the appropriate internal Broker endpoint.
*   **Security Insulation**: It hides the Broker from direct public access, reducing the attack surface. Sensitive operations like credential refresh can be exposed through the Gateway with stricter controls, preventing clients from needing direct access to the Broker.

### 3.2. The Dromos OAuth Broker (The Secure Vault)

The Broker is the core of the framework, handling all stateful and sensitive operations.

*   **Provider Registry**: A database-backed registry of provider profiles, containing all metadata required for authentication.
*   **Consent Engine**: Orchestrates the OAuth 2.0 PKCE flow and generates secure `state` parameters.
*   **Credential Capture Endpoints**: Handles both the standard `/auth/callback` for OAuth 2.0 and a schema-driven flow for generic API key capture.
*   **Encrypted Token Vault**: All credentials are encrypted using AES-GCM and stored in PostgreSQL. It exposes secure, internal endpoints for token retrieval and refresh.

### 3.3. The Bridge (The Universal Client)

The Bridge is the recommended integration path for any Go-based agent or service that needs to maintain a long-lived connection to an external system. It is a "smart client" that automates the entire connection lifecycle.

*   **Multi-Transport Engine**: The Bridge contains robust, production-ready engines for maintaining both **WebSocket** and **gRPC** connections.
*   **Dynamic Authentication**: The Bridge is not hardcoded to any single authentication scheme. On startup, it calls the Dromos Gateway to fetch a generic credential payload. This payload instructs the Bridge on which authentication strategy to use (`"oauth2"`, `"basic_auth"`, `"aws_sigv4"`, etc.) and provides the necessary secrets.
*   **Resilience and Observability**: The Bridge automatically handles dropped connections with an exponential backoff-and-jitter retry strategy. It also comes with built-in, production-ready telemetry, exposing structured logs (Loki-ready) and Prometheus metrics for immediate observability.
*   **How it Fits**: The Bridge acts as the final link in the chain. It consumes the generic credential payloads served by the Broker (via the Gateway) and applies them to outgoing requests, whether they are HTTP-based WebSocket handshakes or gRPC per-RPC credentials. This makes it the executive arm of the Dromos auth strategy, enforcing authentication at the client level.

---

## 4. Key Innovation: Schema-Driven & Generic Credential Management

The framework's most powerful feature is its ability to support any authentication scheme without backend code changes.

This is achieved through two key components:
1.  **Schema-Driven Capture (Broker)**: For non-OAuth providers, the Broker serves a JSON schema that allows a UI to dynamically render a form for capturing any kind of secret (API keys, usernames, etc.).
2.  **Generic Credential Payloads (Broker -> Gateway -> Bridge)**: When a client requests credentials, the Broker constructs a generic JSON response that tells the client *how* to authenticate. The Bridge is designed to interpret this payload and apply the correct authentication strategy at runtime.

This architecture decouples the Broker from the client's authentication logic, allowing for immense operational flexibility and the rapid onboarding of new services.

---

## 5. Use Cases and Benefits

*   **Application Developers**: Are freed from implementing complex auth flows. They can integrate with any service using either the high-level Bridge client (`MaintainWebSocket(...)`) or the low-level Gateway API (`GET /v1/token/...`).
*   **Security Teams**: Gain centralized visibility and control over all external credentials and authentication flows.
*   **Platform Engineers**: Can onboard new services for developers to use by simply adding a configuration to the Broker's database, with no new code deployment required.