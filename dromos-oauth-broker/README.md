# Dromos OAuth Broker

A minimal OAuth 2.0 broker service written in Go that handles OAuth flows for multiple providers, securely storing encrypted tokens in PostgreSQL.

## Features

- **Provider Registration**: Register OAuth providers (Google, GitHub, etc.) with their configuration
- **Consent Flow**: Generate secure OAuth authorization URLs with PKCE and state validation
- **Token Exchange**: Handle OAuth callbacks and securely exchange authorization codes for tokens
- **Token Vault**: AES-GCM encryption for storing OAuth tokens
- **Audit Logging**: Comprehensive audit trail for security and debugging
- **PostgreSQL Storage**: Reliable data persistence with proper indexing

## Quick Start

### 1. Start PostgreSQL

```bash
docker-compose up -d postgres
```

### 2. Run the OAuth Broker

```bash
# Set required environment variables
export DATABASE_URL="postgres://oauth_user:oauth_password@localhost/oauth_broker?sslmode=disable"
export BASE_URL="http://localhost:8080"
export ENCRYPTION_KEY="your-32-byte-base64-encoded-key"
export STATE_KEY="your-32-byte-base64-encoded-hmac-key"

# Run the server
go run ./cmd/oauth-broker
```

### 3. Generate Keys (for development)

```bash
# Generate 32-byte encryption key
openssl rand -base64 32

# Generate 32-byte HMAC key
openssl rand -base64 32
```

## API Endpoints

### Register Provider

Register a new OAuth provider:

```bash
curl -X POST http://localhost:8080/providers \
  -H "Content-Type: application/json" \
  -d '{
    "profile": {
      "name": "Google",
      "client_id": "your-google-client-id",
      "client_secret": "your-google-client-secret",
      "auth_url": "https://accounts.google.com/oauth/authorize",
      "token_url": "https://oauth2.googleapis.com/token",
      "scopes": ["openid", "email", "profile"]
    }
  }'
```

Response:
```json
{
  "id": "provider-uuid",
  "message": "Provider profile created successfully"
}
```

### Request Consent Specification

Get an OAuth authorization URL for a user:

```bash
curl -X POST http://localhost:8080/auth/consent-spec \
  -H "Content-Type: application/json" \
  -d '{
    "workspace_id": "workspace-123",
    "provider_id": "provider-uuid-from-above",
    "scopes": ["openid", "email"],
    "return_url": "https://your-app.com/oauth/callback"
  }'
```

Response:
```json
{
  "authUrl": "https://accounts.google.com/oauth/authorize?client_id=...&response_type=code&scope=openid+email&state=...&code_challenge=...&code_challenge_method=S256",
  "state": "signed-state-token",
  "scopes": ["openid", "email"],
  "provider_id": "provider-uuid"
}
```

### OAuth Callback (handled automatically)

The callback endpoint `/auth/callback` is called by the OAuth provider after user authorization. It:

1. Verifies the state parameter
2. Exchanges the authorization code for tokens
3. Encrypts and stores tokens in the database
4. Updates connection status to "active"
5. Logs audit events
6. Redirects to your return URL

## Environment Variables

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `DATABASE_URL` | PostgreSQL connection string | Yes | - |
| `BASE_URL` | Base URL of the broker service | Yes | - |
| `ENCRYPTION_KEY` | 32-byte base64 key for AES-GCM encryption | No | Auto-generated |
| `STATE_KEY` | 32-byte base64 key for HMAC state signing | No | Auto-generated |
| `PORT` | Server port | No | 8080 |
| `REDIRECT_PATH` | OAuth callback path | No | /auth/callback |

## Database Schema

The service automatically creates these tables:

- `provider_profiles`: OAuth provider configurations
- `connections`: OAuth flow state (pending/active/failed)
- `tokens`: Encrypted OAuth tokens
- `audit_events`: Security and debugging logs

## Sample OAuth Flow

1. **Register Provider**
   ```bash
   # Register Google OAuth provider
   curl -X POST http://localhost:8080/providers \
     -H "Content-Type: application/json" \
     -d '{
       "profile": {
         "name": "Google",
         "client_id": "your-client-id.apps.googleusercontent.com",
         "client_secret": "your-client-secret",
         "auth_url": "https://accounts.google.com/oauth/authorize",
         "token_url": "https://oauth2.googleapis.com/token",
         "scopes": ["openid", "email", "profile"]
       }
     }'
   ```

2. **Get Consent URL**
   ```bash
   # Request OAuth consent specification
   curl -X POST http://localhost:8080/auth/consent-spec \
     -H "Content-Type: application/json" \
     -d '{
       "workspace_id": "my-workspace",
       "provider_id": "google-provider-id",
       "scopes": ["openid", "email"],
       "return_url": "https://myapp.com/oauth/callback"
     }'
   ```

3. **User Authorization**
   - User clicks the `authUrl` and authorizes your application
   - OAuth provider redirects to `/auth/callback` with `code` and `state`

4. **Token Exchange** (automatic)
   - Broker validates state and exchanges code for tokens
   - Tokens are encrypted and stored in database
   - User is redirected to your return URL with success status

## Security Features

- **PKCE (Proof Key for Code Exchange)**: Prevents authorization code interception
- **State Parameter**: CSRF protection with HMAC signing
- **Token Encryption**: AES-GCM encryption for stored tokens
- **Audit Logging**: Complete audit trail of all operations
- **Connection TTL**: Pending connections expire after 10 minutes
- **Input Validation**: Comprehensive validation of all inputs

## Development

### Running Tests

```bash
go test ./...
```

### Building

```bash
go build -o oauth-broker ./cmd/oauth-broker
```

### Database Migrations

The database schema is automatically created when the service starts. For production, consider using a proper migration tool.

## Production Considerations

1. **Use strong encryption keys**: Generate and store secure 32-byte keys
2. **Enable SSL/TLS**: Configure HTTPS for all endpoints
3. **Database security**: Use strong passwords and connection pooling
4. **Monitoring**: Add health checks and metrics
5. **Rate limiting**: Implement rate limiting for API endpoints
6. **Backup strategy**: Regular database backups

## License

This project is open source. See LICENSE file for details.
