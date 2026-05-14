# Authentication Strategies

An authentication strategy defines how the Bridge applies a connection's credentials to an outgoing request. The strategy is stored on the provider profile and returned as part of every token response. Your agent code does not select or configure strategies at runtime — the Bridge reads and applies them automatically.

There are six strategy types.

## oauth2

Injects the `access_token` as an HTTP Bearer token. Used by all standard OAuth2 providers.

```
Authorization: Bearer <access_token>
```

No additional configuration. This strategy is set automatically when you register an OAuth2 provider.

## header

Places a credential value into a named HTTP header with an optional prefix. Use for any provider that authenticates via a custom header.

| Config field | Required | Description |
|---|---|---|
| `header_name` | yes | The header to set |
| `credential_field` | yes | Which key in the credentials map to use |
| `value_prefix` | no | String prepended to the value (e.g. `"Token "`) |

Example — `X-API-Key: <key>`:
```json
{ "header_name": "X-API-Key", "credential_field": "api_key" }
```

Example — `Authorization: Token <key>`:
```json
{ "header_name": "Authorization", "credential_field": "api_key", "value_prefix": "Token " }
```

## query_param

Appends a credential value to the request URL as a query parameter. Not supported for gRPC.

| Config field | Required | Description |
|---|---|---|
| `param_name` | yes | Query parameter name |
| `credential_field` | yes | Which key in the credentials map to use |

## basic_auth

Encodes username and password as HTTP Basic Auth.

```
Authorization: Basic <base64(username:password)>
```

| Config field | Required | Description |
|---|---|---|
| `username_field` | no | Credentials key for the username (default: `"username"`) |
| `password_field` | no | Credentials key for the password (default: `"password"`) |

Supported for gRPC — injects as `authorization` metadata.

## hmac_payload

Signs the request body with HMAC and writes the signature to a header. Used by Stripe, Twilio, GitHub webhooks, and similar request-signing patterns.

| Config field | Required | Description |
|---|---|---|
| `header_name` | yes | Header to write the signature into |
| `secret_field` | yes | Credentials key holding the signing secret |
| `algo` | no | `sha256` (default) or `sha1` |
| `encoding` | no | `hex` (default) or `base64` |

The Bridge reads the body, computes the HMAC, restores the body, and sets the header. If the request has no body, the HMAC is computed over an empty byte slice.

## aws_sigv4

Signs requests with AWS Signature Version 4. Required for all AWS service APIs — S3, DynamoDB, Bedrock, SageMaker, and others.

| Config field | Required | Description |
|---|---|---|
| `region` | no | AWS region (default: `us-east-1`) |
| `service` | yes | AWS service name (e.g. `s3`, `bedrock`) |

The stored credentials must include:

| Credential field | Required | Description |
|---|---|---|
| `access_key` | yes | AWS access key ID |
| `secret_key` | yes | AWS secret access key |
| `session_token` | no | Session token for temporary / assumed-role credentials |

The Bridge sets `X-Amz-Content-Sha256`, computes the payload hash, and calls the AWS SDK's `v4.Signer` to complete full SigV4 signing.
