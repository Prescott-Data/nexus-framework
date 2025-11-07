# OAuth Framework Summary

This document provides a summary of the OAuth 2.0/OIDC integration framework.

## Project Overview

The project is a comprehensive OAuth 2.0/OIDC integration framework built with Go. It follows a microservices architecture and is designed to be a centralized solution for handling authentication with various identity providers.

## Architecture

The framework consists of the following components:

*   **`dromos-oauth-broker`**: The backend service that handles the core OAuth 2.0 flow. This includes provider discovery, consent spec generation (PKCE + signed state), callback/code exchange, token encryption/storage, token retrieval, and token refresh. It also exposes an API for managing providers.
*   **`dromos-oauth-gateway`**: The frontend service that exposes a stable gRPC/HTTP API for clients. It initiates connections via the broker, allows clients to poll for connection status, and fetches tokens on demand. It does not store tokens.
*   **`oauth-sdk`**: A Go SDK for interacting with the OAuth framework's gateway.
*   **`bridge`**: A client-side Go library responsible for creating and maintaining a persistent, authenticated WebSocket connection.
*   **`oauth-demo`**: A demonstration application that showcases how to use the OAuth framework.

## Functionality

### Provider Management

The framework provides a RESTful API for managing OAuth 2.0 providers. This API is protected and requires an API key for access.

*   **Register a Provider:** `POST /providers`
*   **List Providers:** `GET /providers`
*   **Describe a Provider:** `GET /providers/{id}`
*   **Update a Provider:** `PUT /providers/{id}`
*   **Delete a Provider:** `DELETE /providers/{id}` (soft delete)

### Authentication Flow

The authentication flow is based on the OAuth 2.0 Authorization Code Grant with PKCE. The broker handles the interaction with the identity provider, and the gateway provides a simplified interface for clients to initiate the flow and retrieve tokens.

## Non-OAuth Provider Support

The framework is currently designed to support only OAuth 2.0 providers. Adding support for non-OAuth providers would be a high-effort task that would require significant architectural changes. The key areas that would need to be refactored are:

*   **Provider Model:** The current provider data model is tailored to OAuth 2.0 and would need to be redesigned to accommodate other authentication methods.
*   **Authentication Flow:** The core authentication logic is specific to the OAuth 2.0 flow and would need to be replaced with a more flexible system.
*   **Credential Storage:** A more generic credential storage system would be needed to handle different types of secrets.

Implementing this would likely require introducing a new abstraction layer for providers, which would be a fundamental change to the application's architecture.
