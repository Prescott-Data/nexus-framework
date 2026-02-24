<div align="center">
  <h1 align="center">
    Nexus Framework
  </h1>

  <p>
    <a href="https://pkg.go.dev/github.com/Prescott-Data/nexus-framework">
      <img src="https://pkg.go.dev/badge/github.com/Prescott-Data/nexus-framework.svg" alt="Go Reference">
    </a>
    <a href="https://opensource.org/licenses/Apache-2.0">
      <img src="https://img.shields.io/badge/License-Apache_2.0-blue.svg" alt="License">
    </a>
    <a href="https://github.com/Prescott-Data/nexus-framework/discussions">
      <img src="https://img.shields.io/badge/GitHub-Discussions-181717.svg?logo=github" alt="GitHub Discussions">
    </a>
    <a href="https://zenodo.org/records/18315572">
      <img src="https://img.shields.io/badge/Paper-Zenodo-blue.svg" alt="Zenodo Paper">
    </a>
  </p>

<p align="center">
  <strong>🛡️ Zero-Trust Security</strong><br/>
  <strong>🔌 Provider-Agnostic Integration</strong><br/>
  <strong>🚀 Seamless Agent Connectivity</strong><br/>
</p>
  
</div>

---

<br>

## 📌 Nexus: The Universal Integration Layer

The **Nexus Framework** is a provider-agnostic, secure integration layer designed for managing identity and API connections. It seamlessly handles OAuth 2.0, OIDC, API Keys, Basic Auth, and custom static credentials (like AWS SigV4).

By abstracting away the heavy lifting of managing tokens, cryptographic signing, credential refreshes, and provider-specific quirks, Nexus empowers your agents and services to focus purely on **business logic** while enforcing strict **zero-trust security**.

### ✨ Key Features

- **Unified Identity Management**: A single, cohesive layer to manage OAuth 2.0, OIDC, API Keys, Basic Auth, and custom credentials.
- **Provider-Agnostic Architecture**: Connect to any service without rewriting integration logic.
- **Zero-Trust Security**: Secure by default, ensuring all internal and external communications are authenticated and authorized.
- **Automated Lifecycle Management**: Automatically handles token refreshes, cryptographic signing, and credential rotation.
- **Developer-First Abstractions**: Let your agents focus on what they do best, leaving the integration complexity to Nexus.

### 🚀 Quickstart Guide

The fastest way to get your Nexus stack running is with Docker Compose. This spins up the Broker, Gateway, Postgres, and Redis.

```bash
# 1. Configure environment
cp .env.example .env

# 2. Start the stack (pulls official pre-built images)
docker-compose up -d

# Or if you want to develop and build from source:
# docker-compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
```

**Access Points:**

- **Broker**: `http://localhost:8080`
- **Gateway**: `http://localhost:8090`
- **Admin API Key**: Configured in `.env` (Default: `nexus-admin-key`)

> [!NOTE]  
> **Critical Configuration:**
>
> - **`STATE_KEY`**: A base64-encoded 32-byte key used to sign and verify state parameters. **Must be identical across Broker and Gateway services.**
> - **`API_KEYS`**: Comma-separated list of valid API keys for accessing protected Broker endpoints.
> - **`CORS_ALLOWED_ORIGINS`**: Comma-separated list of allowed origins for the Gateway (e.g., `https://app.example.com`).

### 📚 Documentation

Dive deeper into the Nexus Framework with our comprehensive guides:

- 🏗️ **[Architecture](docs/architecture.md)**: System overview, components, and data flow.
- ⚙️ **[Deployment & Config](docs/deployment.md)**: How to configure, build, and deploy the services.
- 🤖 **[Agent Integration Guide](docs/guides/integrating-agents.md)**: How to build agents that consume connections (including the Go Bridge).
- 🔌 **[Provider Management Guide](docs/guides/managing-providers.md)**: How to register and configure identity providers (OAuth2, API Keys).
- 📖 **[API Reference](docs/reference/api.md)**: Links to OpenAPI specifications.
- 🔒 **[Security Model](docs/reference/security-model.md)**: Security guardrails and hardening.

### 🗺️ Roadmap & Updates

<details>
  <summary><b>Click to view our roadmap and upcoming features</b></summary>
  <br>
  For detailed known issues and future plans, please refer to our full <b><a href="ROADMAP.md">Roadmap</a></b>.
  <ul>
    <li>Enhanced Provider Discovery mechanisms</li>
    <li>Expanded non-OAuth integration support</li>
    <li>Improved telemetry and observability</li>
  </ul>
</details>

### 🔗 Quick Links

Explore the individual components of the Nexus ecosystem:

- 🏢 **[Broker Service](nexus-broker/README.md)**: Backend service details.
- 🌐 **[Gateway Service](nexus-gateway/README.md)**: Frontend API service details.
- 🌉 **[Bridge Library](nexus-bridge/README.md)**: Go client library details.

### 📄 Research Paper

If you use the Nexus Framework in your research or project, please check out our foundational paper detailing the architecture and security model:

[![Zenodo Paper](https://img.shields.io/badge/DOI-10.5281/zenodo.18315572-blue.svg)](https://zenodo.org/records/18315572)

### 💬 Community & Support

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) and [Code of Conduct](CODE_OF_CONDUCT.md).

- **Bug Reports & Feature Requests:** Please use [GitHub Issues](https://github.com/Prescott-Data/nexus-framework/issues).
- **Discussions:** Join the conversation on [GitHub Discussions](https://github.com/Prescott-Data/nexus-framework/discussions).

---
<div align="center">
  Maintained with ❤️ by the Prescott Data Team.
</div>
