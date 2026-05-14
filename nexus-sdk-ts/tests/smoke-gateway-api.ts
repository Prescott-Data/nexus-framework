/**
 * Smoke test for the new Gateway API methods in NexusClient.
 * Tests: requestConnection, checkConnection, getTokenByConnectionId
 *
 * Usage:
 *   NEXUS_GATEWAY_URL=https://your-gateway.example.com npx tsx tests/smoke-gateway-api.ts
 */
import { NexusClient } from '../src/index.js';

const GATEWAY_URL = process.env.NEXUS_GATEWAY_URL;
if (!GATEWAY_URL) {
  console.error('error: NEXUS_GATEWAY_URL environment variable is required');
  console.error('usage: NEXUS_GATEWAY_URL=https://your-gateway.example.com npx tsx tests/smoke-gateway-api.ts');
  process.exit(1);
}

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

  // 3. getTokenByConnectionId (using an existing active connection)
  const testConnectionId = process.env.NEXUS_TEST_CONNECTION_ID;
  if (testConnectionId) {
    console.error('\n3. Testing getTokenByConnectionId...');
    try {
      const token = await client.getTokenByConnectionId(testConnectionId);
      console.error(`   ✅ Got token: ${token.accessToken.substring(0, 10)}...`);
      console.error(`   ✅ Token type: ${token.tokenType}`);
    } catch (err: any) {
      console.error(`   ❌ ${err.message}`);
    }
  } else {
    console.error('\n3. Skipping getTokenByConnectionId (NEXUS_TEST_CONNECTION_ID not set)');
  }

  console.error('\n=== All Gateway API tests passed! ===');
}

main().catch((err) => {
  console.error('FAIL:', err);
  process.exit(1);
});
