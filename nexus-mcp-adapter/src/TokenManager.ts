import { LRUCache } from 'lru-cache';
import { NexusTokenInfo } from './types';

export class TokenManager {
  private cache: LRUCache<string, NexusTokenInfo>;

  constructor() {
    this.cache = new LRUCache({
      max: 1000,
      ttl: 1000 * 60 * 60 * 24, // 24 hours max TTL in cache
    });
  }

  private getCacheKey(workspaceId: string, provider: string): string {
    return `${workspaceId}:${provider}`;
  }

  public getToken(workspaceId: string, provider: string): NexusTokenInfo | null {
    const key = this.getCacheKey(workspaceId, provider);
    const tokenInfo = this.cache.get(key);

    if (!tokenInfo) {
      return null;
    }

    const bufferMs = 30 * 1000;
    const now = Date.now();

    if (now > (tokenInfo.expiresAt - bufferMs)) {
      this.cache.delete(key);
      return null;
    }

    return tokenInfo;
  }

  public setToken(workspaceId: string, provider: string, tokenInfo: NexusTokenInfo): void {
    const key = this.getCacheKey(workspaceId, provider);
    this.cache.set(key, tokenInfo);
  }

  public clearToken(workspaceId: string, provider: string): void {
    const key = this.getCacheKey(workspaceId, provider);
    this.cache.delete(key);
  }
}
