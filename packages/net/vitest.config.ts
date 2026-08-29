import { defineTestConfig } from "@repo/vitest-config";

export default defineTestConfig({
  thresholds: { statements: 100, branches: 100, functions: 100, lines: 100 },
});
