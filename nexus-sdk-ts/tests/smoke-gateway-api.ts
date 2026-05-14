/**
 * Smoke test for the new Gateway API methods in NexusClient.
 * Tests: requestConnection, checkConnection, getTokenByConnectionId
 */
import { NexusClient } from '../src/index.js';

const GATEWAY_URL = process.env.NEXUS_GATEWAY_URL || 'https://dromos-oauth-gateway.bravesea-3f5f7e75.eastus.azurecontainerapps.io';

async function main() {
  const client = new NexusClient({
    gatewayUrl: GATEWAY_URL,
    retryPolicy: { retries: 1 },
  });

  console.error('=== Nexus SDK Gateway API Smoke Test ===\n');

  // 1. requestConnection
  console.error('1. Testing requestConnection...');
  const conn = await client.requestConnection({
    userId: 'smoke-test-workspace',
    providerName: 'github',
    scopes: ['repo'],
    returnUrl: `${GATEWAY_URL}/health`,
  });
  console.error(`   ✅ Got authUrl (${conn.authUrl.substring(0, 60)}...)`);
  console.error(`   ✅ connectionId: ${conn.connectionId}`);

  // 2. checkConnection
  console.error('\n2. Testing checkConnection...');
  const status = await client.checkConnection(conn.connectionId);
  console.error(`   ✅ Status: ${status}`);

  // 3. getTokenByConnectionId (using an existing active GitHub connection)
  console.error('\n3. Testing getTokenByConnectionId (existing GitHub connection)...');
  try {
    const token = await client.getTokenByConnectionId('d10f8c19-c468-445f-9fa8-f491e6f6071e');
    console.error(`   ✅ Got token: ${token.accessToken.substring(0, 10)}...`);
    console.error(`   ✅ Token type: ${token.tokenType}`);
  } catch (err: any) {
    console.error(`   ❌ ${err.message}`);
  }

  console.error('\n=== All Gateway API tests passed! ===');
}

main().catch((err) => {
  console.error('FAIL:', err);
  process.exit(1);
});
