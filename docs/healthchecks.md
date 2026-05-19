# Health Checks

The Nexus Broker continuously monitors integration health across two dimensions: **provider-level** (is the upstream API alive?) and **connection-level** (is this user's credential still valid?). Both run as background workers inside the broker process.

---

## Background Workers

### HealthWorker — Provider-Level (5-minute interval)

Probes every registered OAuth2 provider by sending a synthetic `invalid_grant` request to its `token_url`. This deliberately bad request tells us whether the provider's API is reachable and responding to OAuth traffic without requiring a real user credential.

| Provider Response | Status Set |
|-------------------|------------|
| `400 Bad Request` or `401 Unauthorized` | `healthy` — API is alive and rejecting correctly |
| `5xx Server Error` | `unhealthy` — API is down |
| `200 OK` (unexpected for invalid grant) | `degraded` — API behaving abnormally |
| Network error / timeout | `unhealthy` |
| No `token_url` configured | `unknown` |

For non-OAuth2 providers (API key, basic auth), the worker makes a `HEAD` request to `user_info_endpoint` or `api_base_url`. Any non-5xx response is treated as `healthy`.

**Concurrency:** max 10 providers checked concurrently (semaphore + WaitGroup).

---

### ConnectionHealthWorker — Connection-Level (1-minute interval)

Validates individual user connections in batches of 100, prioritising those never checked or longest overdue. Each check has a 15-second timeout.

| Auth Type | Check Method |
|-----------|-------------|
| `oauth2` | Attempt a background token refresh |
| `api_key` / `basic_auth` | Decrypt credential, make a `GET` to `user_info_endpoint` |
| No endpoint configured | Mark `unknown` |

**Provider shielding:** if a refresh or API call fails, the worker cross-references the upstream provider's `health_status` before taking action. If the provider is `unhealthy` or `degraded`, the connection is marked `unhealthy` (retriable) rather than `expired` (terminal). This prevents mass-expiration of valid connections during transient upstream outages.

| Outcome | `health_status` set | `status` changed? |
|---------|--------------------|--------------------|
| Check succeeds | `healthy` | No |
| Fails + provider healthy | `expired` | Yes → `expired` |
| Fails + provider unhealthy/degraded | `unhealthy` | No |
| No endpoint | `unknown` | No |

**Concurrency:** max 20 connections checked concurrently (semaphore + WaitGroup).

---

## `health_status` Values

Both `provider_profiles` and `connections` share the same status vocabulary:

| Value | Meaning |
|-------|---------|
| `healthy` | Last check passed |
| `unhealthy` | Last check failed — retriable |
| `degraded` | Check passed but with unexpected behavior |
| `expired` | Credential confirmed invalid — user must re-authenticate |
| `unknown` | Not yet checked, or not enough information to check |

---

## API Endpoints

### `GET /providers/health`
Returns the health status of all registered providers. No credentials are included.

```http
GET /providers/health
Authorization: X-API-Key <key>
```

```json
[
  {
    "id": "uuid",
    "name": "google",
    "health_status": "healthy",
    "last_health_check_at": "2026-05-19T07:00:00Z",
    "health_message": ""
  },
  {
    "id": "uuid",
    "name": "stripe",
    "health_status": "unhealthy",
    "last_health_check_at": "2026-05-19T07:05:00Z",
    "health_message": "upstream returned 503"
  }
]
```

Returns `[]` (not `null`) when no providers exist.

---

### `GET /connections?workspace_id={workspace_id}`
Returns all non-pending connections for a workspace with health status. No credentials or tokens are included.

```http
GET /connections?workspace_id=ws-123
Authorization: X-API-Key <key>
```

```json
[
  {
    "id": "uuid",
    "provider_id": "uuid",
    "provider_name": "google",
    "auth_type": "oauth2",
    "status": "active",
    "scopes": ["email", "calendar.read"],
    "health_status": "healthy",
    "last_health_check_at": "2026-05-19T07:00:00Z",
    "created_at": "2026-05-01T00:00:00Z",
    "updated_at": "2026-05-19T07:00:00Z"
  }
]
```

**Use case:** Rendering a connections dashboard with live health indicators.

---

### `GET /connections/{id}/token` (enhanced)
The existing token endpoint now includes `health_status` in its response alongside credentials and strategy.

```json
{
  "strategy": { "type": "oauth2" },
  "credentials": { "access_token": "..." },
  "health_status": "healthy"
}
```

**Use case:** Showing an inline warning or re-auth prompt when consuming a credential.

---

## Worker Mode

Health workers run inside the standard broker process. For deployments that need to separate HTTP serving from background polling, pass `--worker-only` to the binary:

```bash
nexus-broker --worker-only
```

In this mode, the HTTP server does not start. The process listens for `SIGINT`/`SIGTERM` and performs a graceful shutdown, draining any in-flight checks before exiting.

The same Docker image and environment variables are used — just override the container command.

---

## Database Schema

```sql
-- provider_profiles
ALTER TABLE provider_profiles
  ADD COLUMN last_health_check_at TIMESTAMP WITH TIME ZONE,
  ADD COLUMN health_status        VARCHAR(50) DEFAULT 'unknown',
  ADD COLUMN health_message       TEXT;

-- connections
ALTER TABLE connections
  ADD COLUMN last_health_check_at TIMESTAMP WITH TIME ZONE,
  ADD COLUMN health_status        VARCHAR(50) DEFAULT 'unknown';

-- Performance index for GetForHealthCheck query
CREATE INDEX IF NOT EXISTS idx_connections_health_check
  ON connections (status, last_health_check_at ASC NULLS FIRST)
  WHERE status = 'active';
```

Migrations: `13_add_provider_health.sql`, `14_add_connection_health.sql`, `15_add_connection_health_index.sql`.