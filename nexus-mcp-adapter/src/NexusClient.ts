import { TokenManager } from './TokenManager';
import { FetcherOptions, NexusClientOptions, NexusTokenInfo } from './types';

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
    console.log(`[NexusClient] Fetching fresh token from Gateway for workspace: ${workspaceId}, provider: ${provider}`);
    
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
    let expiresAt = Date.now() + (1000 * 60 * 60); // Default 1 hour fallback
    if (data.credentials.expires_at) {
        expiresAt = new Date(data.credentials.expires_at).getTime();
    } else if (data.expires_at) {
         expiresAt = new Date(data.expires_at).getTime();
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
      console.log(`[NexusClient] Using cached token for workspace: ${workspaceId}, provider: ${provider}`);
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
      if (tokenInfo.tokenType === 'Bearer') {
        headers.set('Authorization', `Bearer ${tokenInfo.accessToken}`);
      } else {
        // Handle other token types or custom signing strategies (like AWS SigV4) here in the future
        headers.set('Authorization', `${tokenInfo.tokenType} ${tokenInfo.accessToken}`);
      }

      // 3. Create the updated init object
      const updatedInit: RequestInit = {
        ...init,
        headers,
      };

      // 4. Execute the actual fetch request
      console.log(`[NexusClient Fetcher] Executing proxy fetch to ${input.toString()}`);
      return globalThis.fetch(input, updatedInit);
    };
  }
}
