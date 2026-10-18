import { cloudflareTest } from "@cloudflare/vitest-pool-workers";
import { defineTestConfig } from "@repo/vitest-config";

export default defineTestConfig({
  // The handler tests pass their own env, so the "Missing required secrets"
  // warning this prints (wrangler.jsonc's `secrets.required`) is expected.
  plugins: [cloudflareTest({ wrangler: { configPath: "./wrangler.jsonc" } })],
  // Inside workerd, like apps/api - so istanbul, not v8 (see its config).
  provider: "istanbul",
  thresholds: { statements: 90, branches: 80, functions: 90, lines: 90 },
});
