import { describe, expect, it } from "vitest";
import { buildApiCatalogBody, buildSecurityHeaders } from "../src/core";

describe("buildSecurityHeaders", () => {
  it("builds a nonce'd CSP with the requested connect-src additions", () => {
    const { csp, headers, isLocalDev } = buildSecurityHeaders({
      url: new URL("https://tone.rip/"),
      nonce: "abc123",
      connectSrc: ["https://api.github.com"],
    });
    expect(isLocalDev).toBe(false);
    expect(csp).toContain("script-src 'self' 'nonce-abc123'");
    expect(csp).toContain("connect-src 'self' https://api.github.com");
    expect(headers["Strict-Transport-Security"]).toBeTruthy();
  });

  it("relaxes policy on dev hostnames", () => {
    const { csp, headers, isLocalDev } = buildSecurityHeaders({
      url: new URL("http://localhost:4321/"),
      nonce: "abc123",
    });
    expect(isLocalDev).toBe(true);
    expect(csp).toContain("'unsafe-inline'");
    expect(headers["Strict-Transport-Security"]).toBeUndefined();
  });

  it("does not enforce Trusted Types unless asked", () => {
    /* The default matters more than the option does. Enforcing it broke
       production: Cloudflare's JavaScript Detections injects a script into
       the HTML after the Worker runs, reuses our nonce so script-src admits
       it, and then assigns to innerHTML - which a Trusted Types policy
       cannot allow without ceasing to be one. Nothing local reproduces that;
       `wrangler dev` does not run the edge injectors. So the safe value is
       the one you get by not thinking about it. */
    const { csp } = buildSecurityHeaders({
      url: new URL("https://tone.rip/"),
      nonce: "abc123",
    });
    expect(csp).not.toContain("require-trusted-types-for");
    expect(csp).not.toContain("trusted-types");
  });

  it("enforces Trusted Types when asked, allowing exactly one policy", () => {
    const { csp } = buildSecurityHeaders({
      url: new URL("https://tone.rip/"),
      nonce: "abc123",
      trustedTypes: true,
    });
    expect(csp).toContain("require-trusted-types-for 'script'");
    // The name has to match the createPolicy call in @repo/ui's
    // trusted-types.ts. A mismatch throws there and the gradient never starts.
    expect(csp).toContain("trusted-types default");
    // Exactly one, and no `'allow-duplicates'`: a second policy could be
    // created to launder arbitrary strings past the first.
    expect(csp).not.toMatch(/trusted-types [^;]*\s\S+/);
  });

  it("never enforces Trusted Types in dev, even when asked, which would break HMR", () => {
    // Astro's dev client and error overlay are built with innerHTML. The
    // directive would reject the dev server, not the site.
    const { csp } = buildSecurityHeaders({
      url: new URL("http://localhost:4321/"),
      nonce: "abc123",
      trustedTypes: true,
    });
    expect(csp).not.toContain("require-trusted-types-for");
    expect(csp).not.toContain("trusted-types");
  });

  it("defaults Cross-Origin-Resource-Policy to same-origin", () => {
    const { headers } = buildSecurityHeaders({
      url: new URL("https://tone.rip/"),
      nonce: "abc123",
    });
    expect(headers["Cross-Origin-Resource-Policy"]).toBe("same-origin");
  });

  it("allows overriding Cross-Origin-Resource-Policy (apps/api needs cross-origin)", () => {
    const { headers } = buildSecurityHeaders({
      url: new URL("https://api.tone.rip/"),
      nonce: "abc123",
      crossOriginResourcePolicy: "cross-origin",
    });
    expect(headers["Cross-Origin-Resource-Policy"]).toBe("cross-origin");
  });
});

describe("buildApiCatalogBody", () => {
  it("builds an RFC 9727 linkset for the given entries", () => {
    const body = JSON.parse(
      buildApiCatalogBody([{ href: "https://tone.rip/api/projects.json" }]),
    );
    expect(body.linkset[0].anchor).toBe("https://tone.rip/api/projects.json");
  });
});
