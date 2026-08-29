import { defineTestConfig } from "@repo/vitest-config";

export default defineTestConfig({
  thresholds: { statements: 95, branches: 90, functions: 100, lines: 95 },
});
