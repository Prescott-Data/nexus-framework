# Nexus SDK — Python

## v0.2.3 — 2026-05-14

Initial release of the Nexus Python SDK (`nexus-sdk`).

### Features

**Gateway API**
- `request_connection()` — initiate OAuth consent flows
- `check_connection()` — poll connection status
- `get_token_by_connection_id()` — retrieve tokens by connection ID
- `refresh_connection()` — force token refresh
- `wait_for_active()` — poll with configurable interval and timeout

**MCP / Workspace Token Resolution**
- `resolve_token()` — workspace + provider based resolution via `/v1/resolve`
- `get_cached_token()` — cache-aware token retrieval
- `authenticated_fetch()` — HTTP requests with automatic `Authorization` header injection
- `clear_token()` — manual cache eviction

**Infrastructure**
- Thread-safe `TokenCache` with configurable safety buffer
- Configurable `RetryPolicy` with exponential backoff and jitter
- Structured `NexusError` with `code`, `message`, and `status_code`
- Conservative 5-minute TTL fallback when providers omit `expires_at`
- `Bearer` token type normalization per RFC 6750
- All logging to `stderr` via Python `logging` — safe for MCP stdio
- Zero runtime dependencies (stdlib only: `urllib`, `threading`, `logging`)
- Requires Python ≥ 3.11

### Validated Against

- GitHub OAuth (token retrieval + `/user` API call)
- Google OAuth (token retrieval + userinfo)
- Notion OAuth (token retrieval + search API)
