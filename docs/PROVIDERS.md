# Supported Providers

This registry tracks the support status and configuration requirements for all integrated providers.

## Status Audit

We maintain audit logs for provider verification status:

*   **OAuth2 Providers**: [providers_oauth2_audit.csv](../providers_oauth2_audit.csv)
*   **Non-OAuth (API Key/Basic) Providers**: [providers_non_oauth_audit.csv](../providers_non_oauth_audit.csv)

## Provider Configuration Guides

### Airtable
*   **Auth Type**: OAuth2
*   **Documentation**: [Airtable OAuth Reference](https://airtable.com/developers/web/api/oauth-reference)
*   **Notes**: Enterprise scopes differ from Standard plans.

### Airtable API (Personal Access Token)
*   **Auth Type**: `api_key`
*   **Documentation**: [Airtable PATs](https://airtable.com/developers/web/api/personal-access-tokens)
*   **Configuration**:
    ```json
    {
      "auth_type": "api_key",
      "params": {
        "credential_schema": {
           "type": "object",
           "required": ["api_key"],
           "properties": {
             "api_key": { "type": "string", "title": "Personal Access Token" }
           }
        }
      }
    }
    ```

### Asana
*   **Auth Type**: OAuth2
*   **Documentation**: [Asana OAuth](https://developers.asana.com/docs/oauth)

### Snapchat (Snap Kit)
*   **Auth Type**: OAuth2
*   **Documentation**: [Snap Kit (Login Kit)](https://kit.snapchat.com/docs/login-kit)
*   **Auth URL**: `https://accounts.snapchat.com/accounts/oauth2/auth`
*   **Token URL**: `https://accounts.snapchat.com/accounts/oauth2/token`
*   **Scopes**: `https://auth.snapchat.com/oauth2/api/user.display_name`, `https://auth.snapchat.com/oauth2/api/user.bitmoji.avatar`
*   **Portal**: [Snap Kit Developer Portal](https://kit.snapchat.com/portal/)
