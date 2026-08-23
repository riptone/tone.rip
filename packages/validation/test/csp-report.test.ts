import { describe, expect, it } from "vitest";
import { cspReportBodySchema } from "../src/csp-report";

describe("cspReportBodySchema", () => {
  it("accepts a real browser CSP report payload", () => {
    const payload = {
      "csp-report": {
        "document-uri": "https://tone.rip/",
        "violated-directive": "script-src",
        "blocked-uri": "eval",
        "line-number": 12,
      },
    };
    expect(cspReportBodySchema.safeParse(payload).success).toBe(true);
  });

  it("rejects a payload missing the csp-report envelope", () => {
    expect(cspReportBodySchema.safeParse({ foo: "bar" }).success).toBe(false);
  });

  it("rejects wrong-typed fields instead of silently coercing them", () => {
    const payload = { "csp-report": { "line-number": "twelve" } };
    expect(cspReportBodySchema.safeParse(payload).success).toBe(false);
  });
});
