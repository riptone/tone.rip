import { SELF } from "cloudflare:test";
import { describe, expect, it } from "vitest";

describe("app-wide middleware", () => {
  it("serves the RFC 9727 api catalog", async () => {
    const res = await SELF.fetch(
      "https://api.tone.rip/.well-known/api-catalog",
    );
    expect(res.headers.get("Content-Type")).toBe(
      "application/linkset+json; charset=utf-8",
    );
    const body = (await res.json()) as { linkset: { anchor: string }[] };
    expect(body.linkset.map((entry) => entry.anchor)).toEqual([
      "https://api.tone.rip/projects",
      "https://api.tone.rip/projects/{repo}/readme",
      "https://api.tone.rip/status",
      "https://api.tone.rip/csp-report",
      "https://api.tone.rip/info/{slug}",
    ]);
  });

  it("allows the site's own origin cross-origin", async () => {
    const res = await SELF.fetch("https://api.tone.rip/projects", {
      headers: { Origin: "https://tone.rip" },
    });
    expect(res.headers.get("Access-Control-Allow-Origin")).toBe(
      "https://tone.rip",
    );
  });

  it("does not name localhost as a trusted origin in production", async () => {
    // The localhost allowance exists so apps/web can run against this API
    // locally. It used to be unconditional, so the deployed API answered
    // `Access-Control-Allow-Origin: http://localhost:5173` to any page a
    // visitor happened to be serving on their own machine.
    const res = await SELF.fetch("https://api.tone.rip/projects", {
      headers: { Origin: "http://localhost:5173" },
    });
    expect(res.headers.get("Access-Control-Allow-Origin")).toBeNull();
  });

  it("still allows localhost when the worker itself is local", async () => {
    const res = await SELF.fetch("http://localhost:8787/projects", {
      headers: { Origin: "http://localhost:5173" },
    });
    expect(res.headers.get("Access-Control-Allow-Origin")).toBe(
      "http://localhost:5173",
    );
  });

  it("rejects an unrelated origin outright", async () => {
    const res = await SELF.fetch("https://api.tone.rip/projects", {
      headers: { Origin: "https://not-tone.example" },
    });
    expect(res.headers.get("Access-Control-Allow-Origin")).toBeNull();
  });

  it("applies the nonce'd CSP and baseline security headers to every response", async () => {
    const res = await SELF.fetch("https://api.tone.rip/info/tone");
    const csp = res.headers.get("Content-Security-Policy") ?? "";
    expect(csp).toMatch(/script-src 'self' 'nonce-[^']+'/);
    expect(res.headers.get("X-Frame-Options")).toBe("SAMEORIGIN");
  });

  it("renders unknown routes' 404s as application/problem+json, with the shared security headers", async () => {
    const res = await SELF.fetch("https://api.tone.rip/nope");
    expect(res.status).toBe(404);
    expect(res.headers.get("Content-Type")).toBe(
      "application/problem+json; charset=utf-8",
    );
    expect(res.headers.get("X-Content-Type-Options")).toBe("nosniff");
  });
});
