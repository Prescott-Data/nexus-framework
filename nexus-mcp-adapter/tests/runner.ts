/**
 * Nexus MCP Adapter — Test Runner
 *
 * Spawns 5 MCP servers via stdio, connects an MCP client to each,
 * invokes their tool, and reports pass/fail.
 *
 * Prerequisites:
 *   1. Nexus stack running locally (make up)
 *   2. Active connections in the broker for the test workspace_id
 *      for each provider being tested.
 *
 * Usage:
 *   npx tsx tests/runner.ts [--workspace <id>] [--providers github,slack,notion,salesforce,google]
 */

import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// --- Configuration ---

const WORKSPACE_ID = process.env.NEXUS_TEST_WORKSPACE || parseArg("--workspace") || "test-workspace-001";

interface ServerConfig {
  name: string;
  script: string;
  tool: string;
  args: Record<string, string>;
}

const ALL_SERVERS: ServerConfig[] = [
  {
    name: "github",
    script: "servers/github.ts",
    tool: "github_list_repos",
    args: { workspace_id: WORKSPACE_ID },
  },
  {
    name: "slack",
    script: "servers/slack.ts",
    tool: "slack_list_channels",
    args: { workspace_id: WORKSPACE_ID },
  },
  {
    name: "notion",
    script: "servers/notion.ts",
    tool: "notion_search",
    args: { workspace_id: WORKSPACE_ID, query: "test" },
  },
  {
    name: "salesforce",
    script: "servers/salesforce.ts",
    tool: "salesforce_query",
    args: { workspace_id: WORKSPACE_ID, soql: "SELECT Id, Name FROM Account LIMIT 5" },
  },
  {
    name: "google",
    script: "servers/google.ts",
    tool: "google_userinfo",
    args: { workspace_id: WORKSPACE_ID },
  },
];

// Allow filtering providers via CLI: --providers github,slack
const providerFilter = parseArg("--providers");
const selectedProviders = providerFilter
  ? providerFilter.split(",").map((p) => p.trim().toLowerCase())
  : ALL_SERVERS.map((s) => s.name);

const servers = ALL_SERVERS.filter((s) => selectedProviders.includes(s.name));

// --- Helpers ---

function parseArg(flag: string): string | undefined {
  const idx = process.argv.indexOf(flag);
  if (idx !== -1 && idx + 1 < process.argv.length) {
    return process.argv[idx + 1];
  }
  return undefined;
}

interface TestResult {
  server: string;
  passed: boolean;
  detail: string;
  durationMs: number;
}

async function testServer(config: ServerConfig): Promise<TestResult> {
  const start = Date.now();
  const scriptPath = path.resolve(__dirname, config.script);

  let client: Client | null = null;
  let transport: StdioClientTransport | null = null;

  try {
    // 1. Spawn the MCP server as a child process
    transport = new StdioClientTransport({
      command: "npx",
      args: ["tsx", scriptPath],
      env: { ...process.env },
    });

    client = new Client({ name: `test-client-${config.name}`, version: "1.0.0" });
    await client.connect(transport);

    // 2. List tools to verify registration
    const toolsResult = await client.listTools();
    const toolNames = toolsResult.tools.map((t) => t.name);
    if (!toolNames.includes(config.tool)) {
      return {
        server: config.name,
        passed: false,
        detail: `Tool '${config.tool}' not found. Available: [${toolNames.join(", ")}]`,
        durationMs: Date.now() - start,
      };
    }

    // 3. Call the tool
    const result = await client.callTool({ name: config.tool, arguments: config.args });

    // 4. Check result
    const content = result.content as any[];
    const isError = result.isError === true;
    const text = content?.[0]?.text || "";

    if (isError) {
      return {
        server: config.name,
        passed: false,
        detail: text.substring(0, 200),
        durationMs: Date.now() - start,
      };
    }

    // Try to parse as JSON to get a summary
    let summary = text.substring(0, 100);
    try {
      const parsed = JSON.parse(text);
      if (Array.isArray(parsed)) {
        summary = `returned ${parsed.length} items`;
      } else if (parsed.totalSize !== undefined) {
        summary = `returned ${parsed.totalSize} records`;
      } else if (parsed.name) {
        summary = `user: ${parsed.name} (${parsed.email})`;
      } else {
        summary = `returned ${Object.keys(parsed).length} fields`;
      }
    } catch {
      summary = text.substring(0, 80);
    }

    return {
      server: config.name,
      passed: true,
      detail: summary,
      durationMs: Date.now() - start,
    };
  } catch (error: any) {
    return {
      server: config.name,
      passed: false,
      detail: error.message?.substring(0, 200) || "Unknown error",
      durationMs: Date.now() - start,
    };
  } finally {
    try {
      await client?.close();
    } catch { /* ignore cleanup errors */ }
    try {
      await transport?.close();
    } catch { /* ignore */ }
  }
}

// --- Main ---

async function main() {
  console.log("╔══════════════════════════════════════════════════════╗");
  console.log("║       Nexus MCP Adapter — Integration Test Suite    ║");
  console.log("╚══════════════════════════════════════════════════════╝");
  console.log();
  console.log(`  Workspace:  ${WORKSPACE_ID}`);
  console.log(`  Gateway:    ${process.env.NEXUS_GATEWAY_URL || "http://localhost:8090"}`);
  console.log(`  Servers:    ${servers.map((s) => s.name).join(", ")}`);
  console.log();

  // Run all servers in parallel
  console.log("Running tests...\n");
  const results = await Promise.allSettled(servers.map((s) => testServer(s)));

  // Print results
  let passed = 0;
  let failed = 0;

  for (const result of results) {
    if (result.status === "fulfilled") {
      const r = result.value;
      const icon = r.passed ? "✅" : "❌";
      const status = r.passed ? "PASS" : "FAIL";
      console.log(`  ${icon} [${r.server.padEnd(12)}] ${status} (${r.durationMs}ms) — ${r.detail}`);
      r.passed ? passed++ : failed++;
    } else {
      console.log(`  ❌ [unknown     ] FAIL — Promise rejected: ${result.reason}`);
      failed++;
    }
  }

  console.log();
  console.log(`  Results: ${passed} passed, ${failed} failed, ${passed + failed} total`);
  console.log();

  if (failed > 0) {
    console.log("  Some tests failed. Ensure active connections exist for the test workspace.");
    console.log("  Run the consent flow for each provider first:");
    console.log(`    curl -X POST http://localhost:8090/v1/request-connection \\`);
    console.log(`      -H "Content-Type: application/json" \\`);
    console.log(`      -d '{"user_id":"${WORKSPACE_ID}","provider_name":"<provider>","scopes":["..."],"return_url":"http://localhost:3000/callback"}'`);
    process.exit(1);
  }

  console.log("  All tests passed! 🎉");
  process.exit(0);
}

main().catch((err) => {
  console.error("Fatal runner error:", err);
  process.exit(1);
});
