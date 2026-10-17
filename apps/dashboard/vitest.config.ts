import { defineTestConfig } from "@repo/vitest-config";

/* The floors used to be set against the status logic - probes, tailnet
   detection, the resolution table - which is gone along with the status
   column. What is tested now is src/fetch.ts, the only module here with
   decisions in it, and it is nearly fully covered; the floors sit just under
   that so they mean something again rather than describing a deleted suite.
   The one uncovered function is the default `bindNonce`, which needs a built
   Worker's ambient manifest and is the reason the tests inject their own. */
export default defineTestConfig({
  thresholds: { statements: 85, branches: 75, functions: 60, lines: 85 },
});
