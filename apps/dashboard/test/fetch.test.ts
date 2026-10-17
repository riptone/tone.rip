import { describe, expect, it } from "vitest";
import { createDashboardApp } from "../src/fetch";

/**
 * The pipeline with its two production defaults swapped out, as in
 * apps/web/test/fetch.test.ts: `render` stands in for Astro's dispatch and
 * `bindNonce` for the write to `Astro.locals`, both of which need a built
 * Worker's ambient manifest. The header set between them is the real thing.
 *
 * This is the only suite this app has. Everything else it once tested was
 * the status column - probes, tailnet detection, the colours - and went with
 * it. What is left with any logic in it is the policy this file sets, and
 * the one decision in that policy worth guarding is what the page may
 * connect to.
 */
function buildApp() {
  let nonce: string | undefined;
  const app = createDashboardApp({
    bindNonce: (_c, value) => {
      nonce = value;
    },
    render: async (c) => c.html("<html><body>ok</body></html>"),
  });
  return { app, nonce: () => nonce };
}

const directive = (csp: string, name: string) =>
  csp
    .split(";")
    .map((part) => part.trim())
    .find((part) => part.startsWith(`${name} `));

describe("apps/dashboard request pipeline", () => {
  it("publishes a cspNonce to Astro locals and uses it in the policy", async () => {
    const { app, nonce } = buildApp();
    const res = await app.request("https://dash.tone.rip/");
    expect(nonce()).toEqual(expect.any(String));
    expect(res.headers.get("Content-Security-Policy")).toContain(
      `'nonce-${nonce()}'`,
    );
  });

  // It was `https://*.tone.rip` while the browser probed each tile's host.
  // Nothing on the page fetches anything now, so a wildcard reappearing here
  // would be permission for requests nobody is meant to make.
  it("lets the page connect to its own origin and nowhere else", async () => {
    const res = await buildApp().app.request("https://dash.tone.rip/");
    const csp = res.headers.get("Content-Security-Policy") ?? "";
    expect(directive(csp, "connect-src")).toBe("connect-src 'self'");
  });

  // Tile icons are each Access application's logo_url, on hosts this repo
  // does not know - so images are the one place a scheme stands in for a list.
  it("allows https images for the tile icons", async () => {
    const res = await buildApp().app.request("https://dash.tone.rip/");
    const csp = res.headers.get("Content-Security-Policy") ?? "";
    expect(directive(csp, "img-src")?.split(" ")).toContain("https:");
  });

  it("passes the request through to the renderer", async () => {
    const res = await buildApp().app.request("https://dash.tone.rip/");
    expect(await res.text()).toBe("<html><body>ok</body></html>");
  });
});
