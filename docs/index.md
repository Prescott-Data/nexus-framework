# Nexus Framework

**The Universal Integration Layer for AI Agents & Services**

The Nexus Framework is a provider-agnostic, secure integration layer designed for managing identity and API connections. It seamlessly handles OAuth 2.0, OIDC, API Keys, Basic Auth, and custom static credentials (like AWS SigV4), empowering your agents to focus purely on business logic while enforcing strict zero-trust security.

---

## Key Features

- **Unified Identity Management**: A single, cohesive layer to manage OAuth 2.0, OIDC, API Keys, Basic Auth, and custom credentials.
- **Provider-Agnostic Architecture**: Connect to any external service or API without rewriting integration logic for each new provider.
- **Zero-Trust Security**: Secure by default. All internal and external communications are authenticated, authorized, and cryptographically verified.
- **Automated Lifecycle Management**: Automatically handles complex flows like token refreshes, cryptographic signing, and credential rotation.
- **Developer-First Abstractions**: Let your AI agents focus on their core tasks. Nexus abstracts away the integration complexity.

---

## Infrastructure Stack

Nexus is composed of three primary components that work together to provide a seamless integration experience:

| Component | Description | Best For |
| :--- | :--- | :--- |
| **Nexus Broker** | The backend state machine and credential vault. Handles provider configurations, OAuth flows, and secure token storage. | Core Identity Management |
| **Nexus Gateway** | The frontend API service. Acts as the secure entry point for agents to request connections and manage their sessions. | Agent API Access |
| **Nexus Bridge (SDK)** | The native client library (currently available in Go). Simplifies communicating with the Gateway and establishing secure mTLS connections. | Native Agent Integration |

---

## Quick Start

The fastest way to get your Nexus stack running locally is with Docker Compose. This spins up the Broker, Gateway, Postgres, and Redis.

### 1. Configure the Environment
```bash
cp .env.example .env
```
*(Ensure you review the critical configuration variables like `STATE_KEY` and `API_KEYS` in your `.env` file).*

### 2. Start the Stack
```bash
docker-compose up -d
# Or if you want to develop and build from source:
# docker-compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
```

### 3. Verify Access
- **Broker API**: `http://localhost:8080`
- **Gateway API**: `http://localhost:8090`

---

## Deep Dive & Documentation

Explore the comprehensive guides to master the Nexus Framework:

### 🏗️ Architecture & Deployment
*   [**System Architecture**](architecture.md) - Deep dive into components, network topology, and data flow.
*   [**Deployment Guide**](deployment.md) - Learn how to configure, build, and deploy Nexus for production environments.
*   [**Security Model**](reference/security-model.md) - Understand the security guardrails, zero-trust principles, and system hardening.

### 🤖 Integration Guides
*   [**Agent Integration Guide**](guides/integrating-agents.md) - Learn how to build AI agents that securely consume connections via Nexus.
*   [**Provider Management Guide**](guides/managing-providers.md) - Step-by-step instructions on registering and configuring identity providers.

### 📖 References
*   [**API Reference**](reference/api.md) - Explore the full OpenAPI specifications for the Broker and Gateway.
*   [**Roadmap**](ROADMAP.md) - View known issues, planned enhancements, and upcoming features.

---

## Framework Integration

The Nexus Framework is designed to be highly composable and integrates smoothly into larger AI agent ecosystems. By standardizing the connection layer, it allows orchestration frameworks to dynamically provision access to external tools and APIs on behalf of agents, ensuring that credentials never leak into the agent's context.