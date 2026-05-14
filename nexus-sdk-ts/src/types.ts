// ──────────────────────────────────────────────
//  Nexus SDK TypeScript — Type Definitions
// ──────────────────────────────────────────────

// ─── Client Configuration ────────────────────

export interface NexusClientOptions {
  /**
   * The base URL of the Nexus Gateway.
   * e.g., "https://dromos-oauth-gateway.example.com"
   */
  gatewayUrl: string;

  /**
   * Optional API Key for authenticating with the Nexus Gateway.
   */
  apiKey?: string;

  /**
   * Optional retry policy for HTTP requests.
   * If not provided, requests are executed once with no retries.
   */
  retryPolicy?: RetryPolicy;

  /**
   * Optional logger. All diagnostic output goes to stderr by default.
   * Provide a custom logger to control log routing.
   */
  logger?: NexusLogger;
}

// ─── Retry Policy ────────────────────────────

export interface RetryPolicy {
  /**
   * Total retry attempts (excluding the initial request).
   * Total attempts = retries + 1.
   * @default 0
   */
  retries: number;

  /**
   * Base delay in milliseconds for exponential backoff.
   * @default 200
   */
  minDelayMs?: number;

  /**
   * Maximum delay cap in milliseconds.
   * @default 2000
   */
  maxDelayMs?: number;

  /**
   * Whether to also retry on HTTP 429 (Too Many Requests).
   * @default false
   */
  retryOn429?: boolean;
}

// ─── Logger Interface ────────────────────────

export interface NexusLogger {
  info(message: string, ...args: unknown[]): void;
  error(message: string, ...args: unknown[]): void;
  warn(message: string, ...args: unknown[]): void;
}

// ─── Request Connection ──────────────────────

export interface RequestConnectionInput {
  /**
   * The workspace/tenant ID requesting the connection.
   */
  userId: string;

  /**
   * The Nexus provider name (e.g., "github", "slack", "notion").
   */
  providerName: string;

  /**
   * OAuth scopes to request.
   */
  scopes: string[];

  /**
   * URL to redirect the user back to after the OAuth flow completes.
   */
  returnUrl: string;

  /**
   * Optional arbitrary metadata to attach to the connection request.
   */
  metadata?: Record<string, unknown>;
}

export interface RequestConnectionResponse {
  /**
   * The URL to redirect the user to for OAuth consent.
   */
  authUrl: string;

  /**
   * Unique ID for the pending connection (used for polling and token retrieval).
   */
  connectionId: string;

  /**
   * The OAuth state parameter.
   */
  state?: string | undefined;

  /**
   * Scopes granted by the connection.
   */
  scopes?: string[] | undefined;

  /**
   * The internal Nexus provider ID.
   */
  providerId?: string | undefined;
}

// ─── Token Response ──────────────────────────

export interface TokenResponse {
  /**
   * The OAuth access token.
   */
  accessToken: string;

  /**
   * The token type (e.g., "Bearer", "bearer").
   */
  tokenType?: string | undefined;

  /**
   * Number of seconds until the token expires (from the time of issuance).
   */
  expiresIn?: number | undefined;

  /**
   * ISO 8601 timestamp or epoch when the token expires.
   */
  expiresAt?: string | number | undefined;

  /**
   * Scopes granted by the token (space-separated string).
   */
  scope?: string | undefined;

  /**
   * OIDC ID token, if present.
   */
  idToken?: string | undefined;

  /**
   * Refresh token, if present.
   */
  refreshToken?: string | undefined;

  /**
   * Provider name.
   */
  provider?: string | undefined;

  /**
   * Authentication strategy metadata from the broker.
   */
  strategy?: Record<string, unknown> | undefined;

  /**
   * Full credentials payload from the broker (provider-specific fields).
   */
  credentials?: Record<string, unknown> | undefined;

  /**
   * Raw JSON response — contains all fields, including unlisted ones.
   */
  raw?: Record<string, unknown> | undefined;
}

// ─── Cached Token Info (for TokenManager) ────

export interface NexusTokenInfo {
  /**
   * The raw access token / credential value.
   */
  accessToken: string;

  /**
   * The type of token, typically "Bearer".
   * For non-OAuth strategies this may be empty.
   */
  tokenType: string;

  /**
   * Epoch timestamp (in milliseconds) when the token expires.
   */
  expiresAt: number;

  /**
   * The HTTP header name to use for authentication.
   * Defaults to "Authorization". For API-key strategies the broker
   * may specify a different header (e.g., "X-API-Key").
   */
  headerName: string;

  /**
   * The value prefix to prepend before the token.
   * For OAuth2 this is typically "Bearer ". For API-key strategies
   * with no prefix this will be an empty string.
   */
  valuePrefix: string;
}

// ─── Fetcher (MCP-specific) ──────────────────

export interface FetcherOptions {
  /**
   * The Nexus Provider name (e.g., "github", "slack").
   */
  provider: string;

  /**
   * The Workspace ID (tenant ID) representing the user.
   */
  workspaceId: string;
}

// ─── Error Envelope ──────────────────────────

export interface NexusErrorEnvelope {
  /**
   * Machine-readable error code (e.g., "connection_not_found").
   */
  error: string;

  /**
   * Human-readable error message.
   */
  message: string;
}
