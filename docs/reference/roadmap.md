# Roadmap

This page documents what is actively being built, what has been intentionally deferred, and the known limitations of the current implementation. It is kept here rather than only in GitHub Issues so that developers integrating with Nexus can plan their own work against it.

## Active development

### Agent identity and sessions

The core agent auth surface is in development. This includes the agent registry, scoped agent sessions, and OBO delegation.

| Work item | Priority | Description |
|---|---|---|
| Broker: `POST /admin/v1/agents` + agent registry table | High | Register agents with `allowed_scopes`, list and manage the registry |
| Broker: `POST /v1/agent-sessions` + enforcement | High | Short-lived scoped sessions with two-gate scope validation |
| Broker: `POST /v1/agent-sessions/obo` + JWT validation | High | OBO sessions tied to user identity and permission tier |
| Go SDK: `RequestAgentSession`, `RequestOBOSession`, `CloseAgentSession` | Medium | Extend `nexus-sdk/client.go` with agent session methods |
| CLI: `nexus agents list`, `nexus agents register`, `nexus sessions list` | Medium | CLI surface for agent management |
| OpenAPI spec: all new agent endpoints | Low | Document in `openapi.yaml` for SDK generation |

### Python SDK

The Python SDK exists inside `jarviscore` as `jarviscore.nexus.NexusClient`. It is being extracted into a standalone `nexus-sdk` package and published to PyPI. Once published, it will include the agent session methods (`request_agent_session`, `request_obo_session`, `close_agent_session`).

### TypeScript SDK

New package. Same REST API surface as the Go and Python SDKs, TypeScript ergonomics. Will be published to npm as `nexus-sdk` and include agent session support from the initial release.

### Vault backend integration

The Broker currently stores OAuth `client_id` and `client_secret` in PostgreSQL. A `SECRET_BACKEND` configuration option is in development that will allow the Broker to read credentials from external secret managers instead:

| Backend | Config value |
|---|---|
| Internal PostgreSQL (current default) | `SECRET_BACKEND=internal` |
| HashiCorp Vault | `SECRET_BACKEND=hashicorp-vault` |
| AWS Secrets Manager | `SECRET_BACKEND=aws-secrets-manager` |
| GCP Secret Manager | `SECRET_BACKEND=gcp-secret-manager` |

When an external backend is configured, provider registration stores only metadata in PostgreSQL (auth URLs, scopes) — not credentials. Credentials are written to and read from the vault at OAuth flow time. Nexus becomes a pure orchestration layer over your existing secret infrastructure.

### mTLS between Gateway and Broker

The current security model uses an API key for Gateway-to-Broker authentication. Mutual TLS will replace this, providing cryptographically enforced identity at the transport layer. Design is complete. Implementation is in progress.

### OBO session webhooks

When an OBO session is created, closed, or expires, Nexus will emit a webhook event. This allows streaming delegation activity to a SIEM, audit store, or observability pipeline without polling the audit log.

### Sidecar deployment model

For environments requiring zero in-process secret exposure, a Nexus sidecar will intercept outgoing agent requests on `localhost`, fetch credentials from the Gateway, sign the request, and forward it. The agent process holds no credential material. Design phase.

---

## Known limitations

### Audit log has no built-in archival

The audit log grows without bound. There is no TTL or archival mechanism in the Broker. Implement your own archival job. See the [Audit Log reference](audit-log.md) for the recommended approach.

### Static credential rotation requires reconnection

For `api_key` providers, if the credential changes at the provider, there is no automated detection. You update the connection manually via the capture flow.

### Single-region deployment

Nexus does not support multi-region active-active deployments. The `ENCRYPTION_KEY` and PostgreSQL state make cross-region replication non-trivial. Multi-region read replicas for audit log queries are feasible; write operations must route to a single Broker primary.

### No key rotation tooling

`ENCRYPTION_KEY` rotation requires decrypting and re-encrypting every token row. There is no built-in migration command for this. Implement this as an offline script against the database.

---

## Deferred

**SAML provider support.** SAML is a different protocol requiring significant changes to the handshake engine. Out of scope until the core OAuth surface is stable in production.

**RBAC for the admin API.** The Broker's admin API uses a single API key. Fine-grained RBAC is deferred. The immediate mitigation is declarative provider management through `nexus-cli` with git-enforced review gates.
