# Audit Log

The Nexus Broker maintains a tamper-evident audit log of every control-plane mutation. The log is written to the `audit_events` table in PostgreSQL and is queryable through the Broker's REST API.

The audit log records who did what to which resource, and when. It is the primary tool for answering compliance questions ("when was this provider created?"), forensic questions ("why did this agent lose access?"), and operational questions ("which connections were affected by the provider update on the 8th?").

---

## What is logged

Every event in the audit log carries an event type, structured event data, the caller's IP address, and the User-Agent string.

| Event type | When it is written |
|---|---|
| `provider.created` | On every successful `POST /providers` |
| `provider.updated` | On every `PUT` or `PATCH` to a provider |
| `provider.deleted` | On every successful provider deletion |
| `connection.created` | On every successful OAuth callback (token exchange completed) |
| `token.retrieved` | On every successful `GET /connections/{id}/token` |
| `token.exchange_failed` | When the OAuth token exchange fails during a callback |
| `token.storage_failed` | When token encryption or database write fails |
| `token.refresh_fatal` | When a background refresh fails permanently (4xx from provider) |

---

## Querying the audit log

The audit log is available at `GET /audit` on the Broker. All requests require the `X-API-Key` header.

```bash
curl -s "http://localhost:8080/audit" \
  -H "X-API-Key: your-api-key" | jq .
```

### Query parameters

| Parameter | Type | Description |
|---|---|---|
| `event_type` | string | Filter by event type. Example: `provider.deleted` |
| `resource_id` | string | Filter by provider ID or connection ID |
| `since` | ISO 8601 | Return events after this timestamp |
| `until` | ISO 8601 | Return events before this timestamp |
| `limit` | integer | Maximum number of events to return. Default: 50, max: 500 |
| `offset` | integer | Pagination offset |

### Response schema

```json
{
  "events": [
    {
      "id": "evt_01HXYZ...",
      "event_type": "provider.created",
      "created_at": "2026-05-08T09:12:34Z",
      "caller_ip": "10.0.0.2",
      "user_agent": "nexus-cli/0.4.0",
      "data": {
        "provider_id": "prov_01HABC...",
        "provider_name": "google-workspace"
      }
    }
  ],
  "total": 142,
  "limit": 50,
  "offset": 0
}
```

---

## IP address handling

The Broker respects the `X-Forwarded-For` header when recording the caller IP. If your Gateway is behind a load balancer or reverse proxy, ensure the proxy sets `X-Forwarded-For` correctly so the audit log reflects the originating client IP rather than the proxy address.

---

## Retention

The audit log is stored in the same PostgreSQL database as provider and connection data. There is no automatic retention policy. Events are not deleted on a schedule. If you need to bound the size of the audit log, implement a periodic archival job that copies events older than your retention window to cold storage and deletes them from the `audit_events` table.

---

## Using the audit log with nexus-cli

When you manage providers through `nexus-cli apply`, every provider create, update, and delete generates audit log entries. This gives you an automated, traceable record of every declarative change with the `nexus-cli` User-Agent in the `user_agent` field, making it straightforward to distinguish CLI-driven changes from manual API calls.
