import { TokenManager } from './TokenManager.js';
import type { FetcherOptions, NexusClientOptions, NexusTokenInfo } from './types.js';

export class NexusClient {
  private gatewayUrl: string;
  private apiKey: string;
  private tokenManager: TokenManager;

  constructor(options: NexusClientOptions) {
    this.gatewayUrl = options.gatewayUrl.replace(/\/$/, ''); // Remove trailing slash
    this.apiKey = options.apiKey;
    this.tokenManager = new TokenManager();
  }

  /**
   * Internal method to fetch a fresh token from the Nexus Gateway.
   * Currently mocks the response.
   */
  private async fetchTokenFromGateway(workspaceId: string, provider: string): Promise<NexusTokenInfo> {
    console.error(`[NexusClient] Fetching fresh token from Gateway for workspace: ${workspaceId}, provider: ${provider}`);
    
    const url = new URL(`${this.gatewayUrl}/v1/resolve`);
    url.searchParams.append('workspace_id', workspaceId);
    url.searchParams.append('provider_name', provider);

    const response = await fetch(url.toString(), {
      method: 'GET',
      headers: {
        'Accept': 'application/json',
        'X-API-Key': this.apiKey,
      },
    });

    if (!response.ok) {
      const errorText = await response.text();
      throw new Error(`Failed to resolve token from Gateway: ${response.status} ${response.statusText} - ${errorText}`);
    }

    const data = await response.json() as any;

    if (!data.credentials) {
       throw new Error("Invalid response format from Nexus Gateway: missing credentials");
    }

    // Default to Bearer if not explicitly provided or if strategy is header
    let tokenType = 'Bearer';
    if (data.strategy && data.strategy.type === 'header' && data.strategy.config?.value_prefix) {
       tokenType = data.strategy.config.value_prefix.trim();
    } else if (data.credentials.token_type) {
       tokenType = data.credentials.token_type;
    }

    // Attempt to extract the primary token
    let accessToken = data.credentials.access_token;
    
    // Some strategies like API Key might use a different field name based on their config
    if (!accessToken && data.strategy && data.strategy.config?.credential_field) {
        accessToken = data.credentials[data.strategy.config.credential_field];
    }

    if (!accessToken) {
        throw new Error("Invalid credentials format from Nexus Gateway: could not locate access token");
    }

    // Handle expiration parsing
    // Conservative 5-minute default — avoids serving stale tokens if the
    // upstream provider issued a short-lived token (some are 5–15 min).
    let expiresAt = Date.now() + (1000 * 60 * 5);
    if (data.credentials.expires_at) {
        expiresAt = new Date(data.credentials.expires_at).getTime();
    } else if (data.expires_at) {
        expiresAt = new Date(data.expires_at).getTime();
    } else {
        console.warn(`[NexusClient] No expires_at in token response for workspace: ${workspaceId}, provider: ${provider}. Using conservative 5-minute TTL.`);
    }

    return {
      accessToken,
      tokenType,
      expiresAt,
    };
  }

  /**
   * Retrieves a valid token, either from cache or by fetching a fresh one.
   */
  public async getToken(workspaceId: string, provider: string): Promise<NexusTokenInfo> {
    let tokenInfo = this.tokenManager.getToken(workspaceId, provider);

    if (!tokenInfo) {
      tokenInfo = await this.fetchTokenFromGateway(workspaceId, provider);
      this.tokenManager.setToken(workspaceId, provider, tokenInfo);
    } else {
      console.error(`[NexusClient] Using cached token for workspace: ${workspaceId}, provider: ${provider}`);
    }

    return tokenInfo;
  }

  /**
   * Creates a native fetch-compatible function that automatically injects
   * the Nexus authentication headers for the specified workspace and provider.
   */
  public createFetcher(options: FetcherOptions): typeof fetch {
    return async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      // 1. Ensure we have a valid token
      const tokenInfo = await this.getToken(options.workspaceId, options.provider);

      // 2. Prepare the headers, preserving any headers the developer passed in
      const headers = new Headers(init?.headers);
      
      // Inject the authorization header
      // Normalize token type — providers return 'bearer'/'Bearer' inconsistently
      // but RFC 6750 specifies 'Bearer' (capitalized) in the header.
      if (tokenInfo.tokenType.toLowerCase() === 'bearer') {
        headers.set('Authorization', `Bearer ${tokenInfo.accessToken}`);
      } else {
        headers.set('Authorization', `${tokenInfo.tokenType} ${tokenInfo.accessToken}`);
      }

      // 3. Create the updated init object
      const updatedInit: RequestInit = {
        ...init,
        headers,
      };

      // 4. Execute the actual fetch request
      console.error(`[NexusClient Fetcher] Executing proxy fetch to ${input.toString()}`);
      return globalThis.fetch(input, updatedInit);
    };
  }
}
