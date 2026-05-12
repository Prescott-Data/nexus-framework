# Roadmap

This page documents what is actively being built, what has been intentionally deferred, and the known limitations of the current implementation. It is kept in this documentation site rather than only in GitHub Issues so that developers integrating with Nexus can plan their own work against it.

---

## Active work

### mTLS between Gateway and Broker

The current security model uses an API key for Gateway-to-Broker authentication. Mutual TLS will replace this, providing cryptographically enforced identity at the transport layer. This removes the API key as a single-point secret that, if compromised, allows unrestricted Broker access from any host.

The design is complete. Implementation is in progress.

### Gateway-proxied token refresh

Today, if a token expires and you are making direct HTTP calls (not using the Bridge), you call the Broker's refresh endpoint directly to force a refresh before the next `GET /token`. This is an internal endpoint that should not be accessible to agents. The Gateway will expose a proxy endpoint that wraps this operation so agents and non-Go clients can trigger refreshes without needing Broker access.

### OBO session webhooks

When an OBO session is created, closed, or expires, Nexus will emit a webhook event. This allows you to stream delegation activity to your SIEM, audit store, or observability pipeline without polling the audit log.

### TypeScript and Python SDKs

The Go SDK is the reference implementation. TypeScript (`@nexus/sdk`) and Python (`nexus-sdk`) ports are in development. The TypeScript SDK is scheduled for the 0.6 milestone. The Python SDK follows in 0.7.

---

## Known limitations

### No per-scope access control on connections

Currently, when a connection is established, it grants access to all scopes the provider was configured with. There is no mechanism to create a connection scoped to a subset of those scopes per-user. This is relevant for multi-tenant deployments where different users should have different levels of access to the same provider.

The OBO delegation feature addresses this for agent-initiated operations by tying the session to the user's clearance level. Full per-scope connection scoping is on the roadmap but has not been scheduled.

### Static credential rotation requires reconnection

For `api_key` providers, if the underlying credential changes externally (the API key is rotated at the provider), there is no automated mechanism to detect the change and prompt reconnection. You update the connection manually. Automated stale credential detection for static key providers is tracked but not scheduled.

### Audit log has no built-in archival

The audit log grows without bound. There is no TTL or archival mechanism in the Broker. Teams running Nexus in production should implement their own archival job. See the [Audit Log reference](audit-log.md) for the recommended approach.

### Single-region deployment

Nexus does not currently support multi-region active-active deployments. The `ENCRYPTION_KEY` and PostgreSQL state make cross-region replication non-trivial to implement safely. Multi-region read replicas for the audit log query path are feasible today; write operations must route to a single Broker primary.

---

## Deferred features

The following features were considered and explicitly deferred rather than forgotten.

**SAML provider support.** Nexus is designed around OAuth 2.0 and OIDC. SAML is a different protocol with different security properties and a different credential lifecycle. Supporting it within the current Broker architecture would require significant changes to the handshake engine. SAML support is out of scope until the core OAuth surface is stable and well-tested in production.

**Role-based access control for the admin API.** The Broker's admin API currently uses a single API key. Fine-grained RBAC (read-only admin, provider admin, full admin) is deferred. The immediate mitigation is to manage providers declaratively through `nexus-cli` with git-enforced review gates, which provides access control at the process level rather than the API level.
