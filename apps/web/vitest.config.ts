import { defineTestConfig } from "@repo/vitest-config";

export default defineTestConfig({
  environment: "jsdom",
  environmentOptions: { jsdom: { url: "https://tone.rip/" } },
  thresholds: { statements: 85, branches: 75, functions: 80, lines: 85 },
});
