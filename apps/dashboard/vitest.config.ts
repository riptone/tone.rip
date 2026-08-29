import { defineTestConfig } from "@repo/vitest-config";

export default defineTestConfig({
  thresholds: { statements: 55, branches: 75, functions: 40, lines: 50 },
});
