import { cloudflareTest } from "@cloudflare/vitest-pool-workers";
import { defineTestConfig } from "@repo/vitest-config";

export default defineTestConfig({
  plugins: [cloudflareTest({ wrangler: { configPath: "./wrangler.jsonc" } })],
  // v8's coverage API isn't available inside workerd, so this pool needs
  // istanbul's source instrumentation instead - see
  // @cloudflare/vitest-pool-workers' own test suite, which does the same.
  provider: "istanbul",
  thresholds: { statements: 70, branches: 45, functions: 75, lines: 70 },
});
