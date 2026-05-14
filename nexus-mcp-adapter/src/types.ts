export interface NexusClientOptions {
  /**
   * The base URL of the Nexus Gateway
   * e.g., "http://localhost:8080"
   */
  gatewayUrl: string;

  /**
   * The API Key used by this MCP Server to authenticate with the Nexus Gateway
   */
  apiKey: string;
}

export interface NexusTokenInfo {
  /**
   * The raw access token to be used in the Authorization header
   */
  accessToken: string;

  /**
   * The type of token, typically "Bearer"
   */
  tokenType: string;

  /**
   * Epoch timestamp (in milliseconds) when the token expires
   */
  expiresAt: number;
}

export interface FetcherOptions {
  /**
   * The Nexus Provider name (e.g., "github", "slack")
   */
  provider: string;

  /**
   * The Workspace ID (tenant ID) representing the user
   */
  workspaceId: string;
}
