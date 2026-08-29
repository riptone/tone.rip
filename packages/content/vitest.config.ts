import { defineTestConfig } from "@repo/vitest-config";

export default defineTestConfig({
  thresholds: { statements: 85, branches: 70, functions: 95, lines: 90 },
});
