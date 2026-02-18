# Nexus Gateway

The **Nexus Gateway** is the public-facing proxy for the framework. It provides a stable, unified interface for agents to interact with the Nexus Control Plane.

## Core Responsibilities

### 1. Multi-Protocol Support
The Gateway serves the same logic over two protocols:
- **REST (OpenAPI):** Standard HTTP/JSON endpoints for web apps and curl.
- **gRPC:** High-performance binary protocol for internal services and agents.

### 2. Request Translation
The Gateway acts as a "thick proxy" to the Broker:
- **Protocol Buffers:** It defines the official `NexusService` proto.
- **Validation:** It validates request formats before they ever reach the sensitive Broker.
- **Error Mapping:** Translates internal Broker errors into standard HTTP status codes and gRPC status codes.

### 3. Identity Abstraction
The Gateway ensures the Agent never needs to know the Broker exists:
- It signs requests to the Broker using an internal `BROKER_API_KEY`.
- It masks internal database IDs with persistent `connection_id` strings.
- It handles CORS (Cross-Origin Resource Sharing) to allow frontend agents to poll for connection status safely.

### 4. Refresh Proxy
Agents do not call the Broker to refresh tokens. Instead, they call `POST /v1/refresh/{connection_id}` on the Gateway. The Gateway then coordinates the refresh with the Broker and returns the new credentials.

## API Endpoints

| Endpoint | Method | Description |
| :--- | :--- | :--- |
| `/v1/request-connection` | POST | Initiates a new handshake. |
| `/v1/check-connection/{id}`| GET | Returns connection status (pending/active). |
| `/v1/token/{id}` | GET | Returns the current Strategy and Credentials. |
| `/v1/refresh/{id}` | POST | Forces a token refresh. |
| `/v1/providers/metadata` | GET | Returns provider configs for frontend rendering. |
