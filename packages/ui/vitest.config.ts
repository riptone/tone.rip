import { defineTestConfig } from "@repo/vitest-config";

export default defineTestConfig({
  environment: "jsdom",
  thresholds: { statements: 65, branches: 50, functions: 65, lines: 65 },
});
