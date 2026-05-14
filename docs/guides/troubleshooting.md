---
icon: material/lifebuoy
---

# Troubleshooting

Common issues and how to resolve them.

---

## Stack won't start

### Services exit immediately after `make up`

Check the logs:

```bash
docker-compose logs broker
docker-compose logs gateway
```

**`FATAL: STATE_KEY environment variable is required`**

Both the Broker and Gateway require `STATE_KEY` to be set and identical. Copy your `.env.example` to `.env` and fill in `STATE_KEY` with the output of `openssl rand -base64 32`. See [Configuration](../getting-started/configuration.md).

**`DATABASE_URL connection refused`**

PostgreSQL is not healthy yet. Wait for the healthcheck to pass:

```bash
docker-compose ps   # check "healthy" status for nexus-postgres
```

If it never reaches healthy, check that port 5432 is not occupied by another process.

---

## OAuth handshake fails

### Redirect URI mismatch

The provider returns a redirect URI mismatch error during consent.

The provider's developer console has a registered redirect URI that does not match the Broker's callback endpoint. The Broker's callback is:

```
{BASE_URL}/auth/callback
```

Check what `BASE_URL` is set to in your `.env`, construct the callback URL, and verify it is registered exactly in the provider console. Common causes: `http` vs `https`, trailing slash, or wrong port.

### `invalid state` on callback

The Broker logs `invalid or expired state parameter` and redirects to your `return_url` with `status=failed`.

This happens when `STATE_KEY` differs between the Broker and Gateway, or when the state token expires before the user completes consent (15-minute TTL). Verify `STATE_KEY` is identical in both services:

```bash
docker-compose exec broker env | grep STATE_KEY
docker-compose exec gateway env | grep STATE_KEY
```

### OAuth callback returns `failed`

Check the Broker logs for the token exchange error. The most common causes:

| Error | Cause | Fix |
|---|---|---|
| `invalid_grant` | Authorization code already used or expired | The user must restart the consent flow |
| `invalid_client` | Wrong `client_id` or `client_secret` | Verify credentials in the provider console |
| `redirect_uri_mismatch` | Redirect URI not registered | Register `{BASE_URL}/auth/callback` in provider console |
| `access_denied` | User declined consent | Expected — surface a retry to the user |

---

## Credential retrieval fails

### `GET /v1/token/{id}` returns 404

The `connection_id` does not exist in the Broker's database. Verify the connection completed successfully with `GET /v1/check-connection/{id}` first.

### Token returns 200 but the provider API returns 401

The access token is valid according to the Broker but the provider has already invalidated it. Force a refresh:

```bash
curl -s -X POST https://your-gateway.example.com/v1/refresh/CONN_ID \
  -H "X-API-Key: your-api-key"
```

If the refresh returns `attention_required`, the user must reconnect. See [Handling Attention State](attention-state.md).

---

## Agent session errors

### `403 scope_not_permitted_for_agent`

The agent's registered `allowed_scopes` does not include the requested scope. Update the agent's allowed scopes via `PATCH /admin/v1/agents/{id}` on the Broker, or register the agent with the correct scopes from the start.

### `503 backend_auth_unavailable`

The Broker cannot reach `BACKEND_AUTH_URL` to validate the user context token for an OBO session. Verify `BACKEND_AUTH_URL` is set correctly on the Broker and that the endpoint is reachable from within the Docker network.

---

## nexus-cli issues

### `plan` shows all providers as CREATE even after apply

The `name` field in the manifest does not match the `name` stored on the Broker. `nexus-cli` uses `name` as the reconciliation key — it must match exactly. Check the live state:

```bash
curl -s http://localhost:8090/v1/providers \
  -H "X-API-Key: your-api-key" | jq '.[].name'
```

### `apply` fails with `connection refused`

`BROKER_BASE_URL` is set incorrectly or the Broker is not running. Verify:

```bash
curl http://localhost:8080/health
```

---

## Encryption key issues

### All connections return errors after a restart

`ENCRYPTION_KEY` changed between restarts. All stored tokens are encrypted with the original key — a different key makes them permanently unreadable. Restore the original `ENCRYPTION_KEY` value. If it is lost, all connections must be re-established by users going through the consent flow again.

`ENCRYPTION_KEY` must be stored as a managed secret, not in source control. Use your cloud provider's secret manager (AWS Secrets Manager, GCP Secret Manager, Azure Key Vault).

---

## Getting more help

- **GitHub Issues**: [github.com/Prescott-Data/nexus-framework/issues](https://github.com/Prescott-Data/nexus-framework/issues)
- **Discord**: [discord.gg/AbskSXypq](https://discord.gg/AbskSXypq)
- **Audit Log**: Run `GET /audit` on the Broker to see the event trail for any connection or provider operation. See [Audit Log](../reference/audit-log.md).
