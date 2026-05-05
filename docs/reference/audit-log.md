# Audit Log Reference

The Nexus Broker maintains a tamper-evident **audit log** of every control-plane mutation. Every time a provider is created, updated, or deleted — or an OAuth connection is established — a structured record is written to the `audit_events` table.

This provides a queryable history of who changed what and when, which is essential for operating Nexus as critical infrastructure.

---

## Audited Events

| Event Type | Trigger |
| :--- | :--- |
| `provider.created` | A new provider profile is registered via `POST /providers` |
| `provider.updated` | A provider's configuration is modified (`PUT` or `PATCH`) |
| `provider.deleted` | A provider is deleted (by ID or by name) |
| `oauth_flow_completed` | An OAuth callback completes successfully and a connection is established |
| `token_exchange_failed` | The authorization code → token exchange failed |
| `token_storage_failed` | Tokens were exchanged but could not be encrypted/stored |
| `token_retrieved` | A downstream service fetched a connection's token via `GET /connections/{id}/token` |
| `token_retrieval_failed` | A token fetch failed (not found, decryption error, inactive connection, etc.) |
| `token_refresh_fatal` | A refresh token was rejected by the provider (4xx), connection moved to `attention` |
| `oauth_error` | The provider returned an error on the OAuth callback (e.g. `access_denied`) |

---

## Query the Audit Log

```
GET /audit
```

Returns recent audit events in descending chronological order. This endpoint is protected by `ApiKeyMiddleware`.

> **Note:** The Nexus Broker API is unversioned — all routes are mounted at the root (e.g., `/providers`, `/audit`). The `/v1/audit` path referenced elsewhere is aspirational and will apply if/when the Broker adopts a versioned API prefix.

### Query Parameters

| Parameter | Type | Description |
| :--- | :--- | :--- |
| `event_type` | string | Filter by event type (e.g. `provider.deleted`) |
| `since` | string | RFC3339 timestamp — only return events after this time |
| `limit` | integer | Maximum records to return (default: `50`, max: `1000`) |

### Examples

**Fetch the last 50 audit events:**
```bash
curl -s "http://localhost:8080/audit" \
  -H "X-API-Key: <YOUR_API_KEY>" | jq .
```

**Filter by event type:**
```bash
curl -s "http://localhost:8080/audit?event_type=provider.deleted" \
  -H "X-API-Key: <YOUR_API_KEY>" | jq .
```

**Filter by time window:**
```bash
curl -s "http://localhost:8080/audit?since=2026-05-01T00:00:00Z&limit=100" \
  -H "X-API-Key: <YOUR_API_KEY>" | jq .
```

**Combine filters:**
```bash
curl -s "http://localhost:8080/audit?event_type=provider.created&since=2026-05-01T00:00:00Z" \
  -H "X-API-Key: <YOUR_API_KEY>" | jq .
```

---

## Response Schema

```json
[
  {
    "id": "a1b2c3d4-...",
    "connection_id": "f5e6d7c8-...",
    "event_type": "oauth_flow_completed",
    "event_data": "{\"provider_id\": \"...\", \"workspace_id\": \"ws-123\"}",
    "ip_address": "10.0.0.1",
    "user_agent": "nexus-gateway/1.0",
    "created_at": "2026-05-05T10:30:00Z"
  },
  {
    "id": "b2c3d4e5-...",
    "connection_id": null,
    "event_type": "provider.deleted",
    "event_data": "{\"provider_id\": \"...\", \"provider_name\": \"old-slack\"}",
    "ip_address": "192.168.1.5",
    "user_agent": "curl/7.88.1",
    "created_at": "2026-05-05T09:15:00Z"
  }
]
```

### Field Descriptions

| Field | Type | Description |
| :--- | :--- | :--- |
| `id` | UUID | Unique audit event identifier |
| `connection_id` | UUID \| null | Associated connection, if applicable |
| `event_type` | string | The event type (see table above) |
| `event_data` | string \| null | JSON payload with event-specific context |
| `ip_address` | string \| null | IP of the caller (respects `X-Forwarded-For`) |
| `user_agent` | string \| null | User-Agent of the caller |
| `created_at` | RFC3339 | Timestamp of the event |

---

## Database

Audit events are stored in the `audit_events` PostgreSQL table, created in the initial migration (`00_create_tables.sql`). An index on `created_at DESC` (migration `11_add_audit_created_at_index.sql`) ensures fast time-range queries even at high volume.

!!! note "Retention Policy"
    There is currently no automatic retention/pruning policy for audit events. For long-running production deployments, consider adding a scheduled job to archive or delete records older than your compliance window (e.g., 90 days).

---

## Audit via `nexus-cli`

Every mutation performed by [`nexus-cli apply`](../guides/security-as-code.md) is automatically recorded in the audit log. You can correlate CLI runs with audit events using the `ip_address` field (the IP of your CI runner) and the `event_data.provider_name` field.

```bash
# See all provider changes from a CI apply run
curl -s "http://localhost:8080/audit?event_type=provider.created&since=2026-05-05T13:00:00Z" \
  -H "X-API-Key: <YOUR_API_KEY>" | jq .
```
