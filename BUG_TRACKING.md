# Bug Tracking

## Bug: Refreshed tokens are not persisted, causing stale token retrieval

- **Date Reported:** 2025-10-22
- **Status:** Resolved

### Description

When `POST /connections/{id}/refresh` is called, the broker successfully refreshes the token with the provider (e.g., Google) but fails to update the database with the new token. As a result, subsequent calls to `GET /connections/{id}/token` return the old, expired token.

### Root Cause

The `GetToken` function in `dromos-oauth-broker/internal/handlers/callback.go` was not explicitly ordering the tokens by creation date. When multiple tokens existed for a single connection (as is the case after a refresh), the database's default retrieval order was not guaranteed, leading to an older token being returned.

### Solution

The SQL query in the `GetToken` function was updated to include `ORDER BY created_at DESC LIMIT 1`. This ensures that the most recent token is always selected, resolving the issue of stale data.

### Verification

The fix was verified by adding a temporary test case that creates multiple tokens for a connection and asserts that `GetToken` returns the latest one. The test passed, confirming the solution is effective.

