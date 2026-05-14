# Handling Attention State

A connection enters `attention` state when the Broker attempts to refresh the access token and the provider responds with a `4xx` error. This indicates the user's authorization grant has been revoked, the refresh token has expired, or the provider requires the user to re-authorize.

This guide covers how to detect `attention` state, surface it to the user, and restore the connection.

## How attention state is triggered

During a token refresh (`POST /connections/{id}/refresh`), the Broker calls the provider's token endpoint. If the provider returns a `4xx` response, the Broker:

1. Updates the connection status to `attention`.
2. Returns an `attention_required` error to the caller (the Bridge or your backend).
3. Writes an `attention_required` event to the audit log.

The stored access token may still be valid for the remainder of its lifetime. Once it expires, the connection becomes unusable.

## Detecting attention state

### Via the Bridge

The Bridge returns `ErrInteractionRequired` when it detects `attention_required` during a refresh. The retry loop stops immediately — the Bridge does not retry because retrying cannot help.

```go
err := bridge.MaintainWebSocket(ctx, connectionID, endpointURL, handler)
if errors.Is(err, nexusbridge.ErrInteractionRequired) {
    // Connection is in attention state.
    // Notify your application to re-authorize this connection.
    notifyReconsentRequired(connectionID)
}
```

### Via the SDK

If you are managing the refresh cycle manually, check the error code:

```go
_, err := client.RefreshConnection(ctx, connectionID)
if err != nil {
    var e oauthsdk.ErrorEnvelope
    if errors.As(err, &e) && e.Code == "attention_required" {
        notifyReconsentRequired(connectionID)
        return
    }
    // Other errors are transient — retry according to your policy.
}
```

### Via polling

If your application polls connection status, `check-connection` returns `attention` for connections in this state:

```bash
curl -s "https://your-gateway.example.com/v1/check-connection/CONNECTION_ID" \
  -H "X-API-Key: your-gateway-api-key"
# {"status": "attention"}
```

## Notifying the user

`attention` state is a user-facing event. The user who authorized the connection must re-authorize it. Your application is responsible for surfacing this.

Recommended pattern:

1. Store the `connection_id` alongside the `workspace_id` in your application database.
2. When `attention_required` is detected, mark the connection as needing re-authorization in your database.
3. On the user's next session, display a re-authorization prompt identifying which provider needs attention.
4. Initiate a new consent flow for that workspace and provider.

Do not silently swallow `attention_required` errors or retry indefinitely. Users will lose access to the agent's capabilities without knowing why.

## Re-authorizing the connection

Initiate a new consent flow for the same `workspace_id` and `provider_id`:

```bash
curl -s -X POST https://your-gateway.example.com/v1/request-connection \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-gateway-api-key" \
  -d '{
    "workspace_id": "user_abc",
    "provider_id": "SAME_PROVIDER_UUID",
    "scopes": ["openid", "email", "offline_access"],
    "return_url": "https://your-app.com/connections/callback"
  }' | jq .
```

This creates a new connection with a new `connection_id`. The old connection in `attention` state remains in the database until you delete it or the cleanup job removes it.

Update your application's connection registry to use the new `connection_id` for this workspace and provider. Restart any Bridge instances using the old connection ID with the new one.

## Cleaning up the old connection

The old connection in `attention` state does not delete itself. Delete it explicitly after the new connection is active:

```bash
# Verify the new connection is active first
curl -s "https://your-gateway.example.com/v1/check-connection/NEW_CONNECTION_ID" \
  -H "X-API-Key: your-gateway-api-key"
# {"status": "active"}

# Then delete the old one
curl -s -X DELETE "https://your-gateway.example.com/v1/connections/OLD_CONNECTION_ID" \
  -H "X-API-Key: your-gateway-api-key"
```

## Common causes of attention state

| Cause | How to confirm | Resolution |
|---|---|---|
| User revoked access in the provider's settings | Audit log shows `token_refresh_fatal` with `400` or `401` status | User must re-authorize |
| Refresh token expired (some providers set a max lifetime) | Same as above | User must re-authorize |
| Provider OAuth app credentials rotated | All connections for this provider enter `attention` | Update `client_secret` on the provider profile with `PATCH /v1/providers/{id}`, then users re-authorize |
| Provider API outage | Audit log shows `token_refresh_fatal` with `5xx` status | This should not set `attention` — only `4xx` triggers it. File a bug if you see `5xx` causing attention state. |
