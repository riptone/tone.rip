import { SELF } from "cloudflare:test";
import { describe, expect, it } from "vitest";

describe("POST /csp-report", () => {
  it("accepts a well-formed CSP report and returns 202", async () => {
    const res = await SELF.fetch("https://api.tone.rip/csp-report", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        "csp-report": {
          "document-uri": "https://tone.rip/",
          "violated-directive": "script-src",
          "blocked-uri": "eval",
        },
      }),
    });
    expect(res.status).toBe(202);
    expect(await res.json()).toEqual({ ok: true });
  });

  it("rejects a payload missing the csp-report envelope as a validation problem", async () => {
    const res = await SELF.fetch("https://api.tone.rip/csp-report", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ nope: true }),
    });
    expect(res.status).toBe(400);
    expect(res.headers.get("Content-Type")).toBe(
      "application/problem+json; charset=utf-8",
    );
  });

  it("GET is a cheap liveness check", async () => {
    const res = await SELF.fetch("https://api.tone.rip/csp-report");
    expect(res.status).toBe(200);
  });
});
