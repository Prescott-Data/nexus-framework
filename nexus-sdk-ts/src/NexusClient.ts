import { TokenManager } from './TokenManager.js';
import type {
  FetcherOptions,
  NexusClientOptions,
  NexusErrorEnvelope,
  NexusLogger,
  NexusTokenInfo,
  RequestConnectionInput,
  RequestConnectionResponse,
  RetryPolicy,
  TokenResponse,
} from './types.js';

// ──────────────────────────────────────────────
//  Default stderr logger — safe for MCP stdio
// ──────────────────────────────────────────────

const defaultLogger: NexusLogger = {
  info: (msg, ...args) => console.error(`[NexusSDK] ${msg}`, ...args),
  error: (msg, ...args) => console.error(`[NexusSDK:ERROR] ${msg}`, ...args),
  warn: (msg, ...args) => console.error(`[NexusSDK:WARN] ${msg}`, ...args),
};

// ──────────────────────────────────────────────
//  NexusClient — Unified TypeScript SDK
// ──────────────────────────────────────────────

export class NexusClient {
  private readonly gatewayUrl: string;
  private readonly apiKey: string;
  private readonly tokenManager: TokenManager;
  private readonly retryPolicy: Required<RetryPolicy>;
  private readonly logger: NexusLogger;

  constructor(options: NexusClientOptions) {
    this.gatewayUrl = options.gatewayUrl.replace(/\/$/, '');
    this.apiKey = options.apiKey ?? '';
    this.tokenManager = new TokenManager();
    this.logger = options.logger ?? defaultLogger;
    this.retryPolicy = NexusClient.normalizeRetryPolicy(options.retryPolicy);
  }

  // ─── Retry Policy Normalization ──────────────

  private static normalizeRetryPolicy(
    policy?: RetryPolicy,
  ): Required<RetryPolicy> {
    return {
      retries: Math.max(policy?.retries ?? 0, 0),
      minDelayMs: Math.max(policy?.minDelayMs ?? 200, 1),
      maxDelayMs: Math.max(policy?.maxDelayMs ?? 2000, 1),
      retryOn429: policy?.retryOn429 ?? false,
    };
  }

  // ─── Core HTTP Helper with Retries ───────────

  private async doRequest(
    method: string,
    url: string,
    body?: unknown,
  ): Promise<Response> {
    const pol = this.retryPolicy;

    const attempt = async (): Promise<Response> => {
      const headers: Record<string, string> = {
        Accept: 'application/json',
      };
      if (this.apiKey) {
        headers['X-API-Key'] = this.apiKey;
      }

      const init: RequestInit = { method, headers };
      if (body !== undefined) {
        headers['Content-Type'] = 'application/json';
        init.body = JSON.stringify(body);
      }

      const response = await globalThis.fetch(url, init);

      if (response.ok) {
        return response;
      }

      // Classify retryable statuses
      const retryable =
        response.status === 502 ||
        response.status === 503 ||
        response.status === 504 ||
        (pol.retryOn429 && response.status === 429);

      if (!retryable) {
        // Non-retryable error — parse and throw immediately
        const errorText = await response.text();
        let parsed: NexusErrorEnvelope | undefined;
        try {
          parsed = JSON.parse(errorText) as NexusErrorEnvelope;
        } catch {
          // Not JSON — fall through
        }
        const message = parsed?.message ?? errorText;
        const code = parsed?.error ?? `http_${response.status}`;
        throw new NexusError(code, message, response.status);
      }

      // Retryable — consume body and signal for retry
      await response.text();
      throw new RetryableError(response.status);
    };

    let lastError: Error | undefined;

    for (let i = 0; i <= pol.retries; i++) {
      if (i > 0) {
        this.logger.info(`Attempt ${i + 1}/${pol.retries + 1}: ${method} ${url}`);
      }

      try {
        return await attempt();
      } catch (err) {
        lastError = err as Error;

        if (!(err instanceof RetryableError)) {
          throw err; // Non-retryable — propagate immediately
        }

        if (i === pol.retries) {
          throw new NexusError(
            'max_retries_exceeded',
            `Request failed after ${pol.retries + 1} attempts: ${method} ${url}`,
            (err as RetryableError).statusCode,
          );
        }

        // Exponential backoff with jitter
        const delay = this.backoff(i, pol.minDelayMs, pol.maxDelayMs);
        this.logger.info(`Retrying in ${delay}ms: ${lastError.message}`);
        await NexusClient.sleep(delay);
      }
    }

    // Should never reach here, but TypeScript needs the return path
    throw lastError ?? new Error('Unexpected retry loop exit');
  }

  private backoff(attempt: number, minDelay: number, maxDelay: number): number {
    const capped = Math.min(attempt, 10);
    const factor = 1 << capped; // 2^attempt
    const base = minDelay * factor;
    const bounded = Math.min(base, maxDelay);
    const jitter = 0.2 + Math.random() * 0.6; // 0.2..0.8
    return Math.floor(bounded * jitter);
  }

  private static sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }

  // ─────────────────────────────────────────────
  //  Gateway API Methods (Go SDK parity)
  // ─────────────────────────────────────────────

  /**
   * Initiates an OAuth connection request.
   * Wraps POST /v1/request-connection
   *
   * @returns The auth URL to redirect the user to, along with the connection ID.
   */
  public async requestConnection(
    input: RequestConnectionInput,
  ): Promise<RequestConnectionResponse> {
    const payload = {
      user_id: input.userId,
      provider_name: input.providerName,
      scopes: input.scopes,
      return_url: input.returnUrl,
      metadata: input.metadata,
    };

    const resp = await this.doRequest(
      'POST',
      `${this.gatewayUrl}/v1/request-connection`,
      payload,
    );

    const data = (await resp.json()) as {
      authUrl: string;
      connection_id: string;
      state?: string;
      scopes?: string[];
      provider_id?: string;
    };

    return {
      authUrl: data.authUrl,
      connectionId: data.connection_id,
      state: data.state,
      scopes: data.scopes,
      providerId: data.provider_id,
    };
  }

  /**
   * Checks the status of a connection.
   * Wraps GET /v1/check-connection/{connectionId}
   *
   * @returns The status string (e.g., "active", "pending", "failed").
   */
  public async checkConnection(connectionId: string): Promise<string> {
    if (!connectionId.trim()) {
      throw new NexusError('invalid_input', 'connectionId must not be empty');
    }

    const resp = await this.doRequest(
      'GET',
      `${this.gatewayUrl}/v1/check-connection/${encodeURIComponent(connectionId)}`,
    );

    const data = (await resp.json()) as { status: string };
    return data.status;
  }

  /**
   * Retrieves a token by connection ID.
   * Wraps GET /v1/token/{connectionId}
   *
   * @returns The full token response from the broker.
   */
  public async getTokenByConnectionId(
    connectionId: string,
  ): Promise<TokenResponse> {
    if (!connectionId.trim()) {
      throw new NexusError('invalid_input', 'connectionId must not be empty');
    }

    const resp = await this.doRequest(
      'GET',
      `${this.gatewayUrl}/v1/token/${encodeURIComponent(connectionId)}`,
    );

    const raw = (await resp.json()) as Record<string, unknown>;
    return NexusClient.parseTokenResponse(raw);
  }

  /**
   * Forces a token refresh for a connection.
   * Wraps POST /v1/refresh/{connectionId}
   *
   * @returns The refreshed token response.
   */
  public async refreshConnection(
    connectionId: string,
  ): Promise<TokenResponse> {
    if (!connectionId.trim()) {
      throw new NexusError('invalid_input', 'connectionId must not be empty');
    }

    const resp = await this.doRequest(
      'POST',
      `${this.gatewayUrl}/v1/refresh/${encodeURIComponent(connectionId)}`,
    );

    const raw = (await resp.json()) as Record<string, unknown>;
    return NexusClient.parseTokenResponse(raw);
  }

  /**
   * Polls check-connection until the status is "active" or "failed",
   * or until the AbortSignal fires.
   *
   * @param connectionId - The connection ID to poll.
   * @param intervalMs - Polling interval in milliseconds (default: 1500ms).
   * @param signal - Optional AbortSignal for cancellation/timeout.
   * @returns The terminal status ("active" or "failed").
   */
  public async waitForActive(
    connectionId: string,
    intervalMs: number = 1500,
    signal?: AbortSignal,
  ): Promise<string> {
    while (true) {
      if (signal?.aborted) {
        throw new NexusError('aborted', 'waitForActive was aborted');
      }

      const status = await this.checkConnection(connectionId);

      if (status === 'active' || status === 'failed') {
        return status;
      }

      // Wait before next poll
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(resolve, intervalMs);
        if (signal) {
          signal.addEventListener(
            'abort',
            () => {
              clearTimeout(timer);
              reject(new NexusError('aborted', 'waitForActive was aborted'));
            },
            { once: true },
          );
        }
      });
    }
  }

  // ─────────────────────────────────────────────
  //  MCP Token Resolution (workspace + provider)
  // ─────────────────────────────────────────────

  /**
   * Resolves a token from the gateway using workspace ID + provider name.
   * This is the primary method used by MCP servers for dynamic auth injection.
   * Wraps GET /v1/resolve?workspace_id=...&provider_name=...
   */
  public async resolveToken(
    workspaceId: string,
    provider: string,
  ): Promise<NexusTokenInfo> {
    this.logger.info(
      `Resolving token for workspace: ${workspaceId}, provider: ${provider}`,
    );

    const url = new URL(`${this.gatewayUrl}/v1/resolve`);
    url.searchParams.append('workspace_id', workspaceId);
    url.searchParams.append('provider_name', provider);

    const resp = await this.doRequest('GET', url.toString());
    const data = (await resp.json()) as Record<string, unknown>;

    const credentials = data.credentials as
      | Record<string, unknown>
      | undefined;
    if (!credentials) {
      throw new NexusError(
        'invalid_response',
        'Invalid response format from Nexus Gateway: missing credentials',
      );
    }

    // Determine token type
    let tokenType = 'Bearer';
    const strategy = data.strategy as Record<string, unknown> | undefined;
    if (
      strategy?.type === 'header' &&
      (strategy.config as Record<string, unknown>)?.value_prefix
    ) {
      tokenType = String(
        (strategy.config as Record<string, unknown>).value_prefix,
      ).trim();
    } else if (credentials.token_type) {
      tokenType = String(credentials.token_type);
    }

    // Extract access token
    let accessToken = credentials.access_token as string | undefined;
    if (
      !accessToken &&
      strategy &&
      (strategy.config as Record<string, unknown>)?.credential_field
    ) {
      accessToken = credentials[
        String((strategy.config as Record<string, unknown>).credential_field)
      ] as string | undefined;
    }

    if (!accessToken) {
      throw new NexusError(
        'invalid_credentials',
        'Could not locate access token in Nexus Gateway response',
      );
    }

    // Parse expiration — conservative 5-minute default
    let expiresAt = Date.now() + 1000 * 60 * 5;
    if (credentials.expires_at) {
      expiresAt = new Date(String(credentials.expires_at)).getTime();
    } else if (data.expires_at) {
      expiresAt = new Date(String(data.expires_at)).getTime();
    } else {
      this.logger.warn(
        `No expires_at in token response for workspace: ${workspaceId}, provider: ${provider}. Using conservative 5-minute TTL.`,
      );
    }

    return { accessToken, tokenType, expiresAt };
  }

  /**
   * Retrieves a valid cached token, or fetches a fresh one via resolveToken.
   * This is the high-level method used by MCP servers.
   */
  public async getToken(
    workspaceId: string,
    provider: string,
  ): Promise<NexusTokenInfo> {
    let tokenInfo = this.tokenManager.getToken(workspaceId, provider);

    if (!tokenInfo) {
      tokenInfo = await this.resolveToken(workspaceId, provider);
      this.tokenManager.setToken(workspaceId, provider, tokenInfo);
    } else {
      this.logger.info(
        `Using cached token for workspace: ${workspaceId}, provider: ${provider}`,
      );
    }

    return tokenInfo;
  }

  /**
   * Clears a cached token, forcing the next getToken call to fetch fresh.
   */
  public clearToken(workspaceId: string, provider: string): void {
    this.tokenManager.clearToken(workspaceId, provider);
  }

  // ─────────────────────────────────────────────
  //  Fetch Proxy (MCP-specific)
  // ─────────────────────────────────────────────

  /**
   * Creates a native fetch-compatible function that automatically injects
   * Nexus authentication headers for the specified workspace and provider.
   *
   * This is the primary integration point for MCP server tool handlers.
   *
   * @example
   * ```typescript
   * const fetcher = nexusClient.createFetcher({
   *   workspaceId: "ws-001",
   *   provider: "github",
   * });
   *
   * const repos = await fetcher("https://api.github.com/user/repos");
   * ```
   */
  public createFetcher(options: FetcherOptions): typeof fetch {
    return async (
      input: RequestInfo | URL,
      init?: RequestInit,
    ): Promise<Response> => {
      const tokenInfo = await this.getToken(
        options.workspaceId,
        options.provider,
      );

      const headers = new Headers(init?.headers);

      // Normalize token type per RFC 6750
      if (tokenInfo.tokenType.toLowerCase() === 'bearer') {
        headers.set('Authorization', `Bearer ${tokenInfo.accessToken}`);
      } else {
        headers.set(
          'Authorization',
          `${tokenInfo.tokenType} ${tokenInfo.accessToken}`,
        );
      }

      const updatedInit: RequestInit = { ...init, headers };

      this.logger.info(`Fetcher → ${input.toString()}`);
      return globalThis.fetch(input, updatedInit);
    };
  }

  // ─── Internal Helpers ────────────────────────

  private static parseTokenResponse(
    raw: Record<string, unknown>,
  ): TokenResponse {
    const credentials = raw.credentials as
      | Record<string, unknown>
      | undefined;

    return {
      accessToken:
        (raw.access_token as string) ??
        (credentials?.access_token as string) ??
        '',
      tokenType: (raw.token_type as string) ?? (credentials?.token_type as string),
      expiresIn: raw.expires_in as number | undefined,
      expiresAt: (raw.expires_at as string | number) ?? (credentials?.expires_at as string | number),
      scope: raw.scope as string | undefined,
      idToken: (raw.id_token as string) ?? (credentials?.id_token as string),
      refreshToken:
        (raw.refresh_token as string) ??
        (credentials?.refresh_token as string),
      provider: raw.provider as string | undefined,
      strategy: raw.strategy as Record<string, unknown> | undefined,
      credentials,
      raw,
    };
  }
}

// ──────────────────────────────────────────────
//  Error Classes
// ──────────────────────────────────────────────

export class NexusError extends Error {
  public readonly code: string;
  public readonly statusCode?: number;

  constructor(code: string, message: string, statusCode?: number) {
    super(`${code}: ${message}`);
    this.name = 'NexusError';
    this.code = code;
    this.statusCode = statusCode;
  }
}

/** Internal sentinel — only used inside the retry loop. */
class RetryableError extends Error {
  public readonly statusCode: number;
  constructor(statusCode: number) {
    super(`Retryable HTTP ${statusCode}`);
    this.name = 'RetryableError';
    this.statusCode = statusCode;
  }
}
