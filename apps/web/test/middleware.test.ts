import { describe, expect, it } from "vitest";
import { onRequest } from "../src/middleware";

// Minimal stand-in for Astro's MiddlewareHandler context - just enough of
// APIContext for this middleware's own logic (it never touches routing,
// props, etc).
function buildContext(url: string, init?: RequestInit) {
  return {
    request: new Request(url, init),
    locals: {} as { cspNonce?: string },
  };
}

const next = async () =>
  new Response("<html><body>ok</body></html>", {
    status: 200,
    headers: { "Content-Type": "text/html" },
  });

/**
 * Run the middleware and insist on a Response.
 *
 * Astro types a MiddlewareHandler as returning `Response | void`, so every
 * assertion below would otherwise be reading properties off `void`. Narrowing
 * it here is not just appeasing the compiler: "this middleware always answers
 * with a Response" is a real property of it, and this is where it gets
 * checked.
 */
async function run(ctx: ReturnType<typeof buildContext>): Promise<Response> {
  const res = await onRequest(ctx as never, next);
  if (!(res instanceof Response)) {
    throw new Error("middleware returned no Response");
  }
  return res;
}

describe("apps/web middleware", () => {
  it("sets a cspNonce on locals", async () => {
    const ctx = buildContext("https://tone.rip/");
    await run(ctx);
    expect(ctx.locals.cspNonce).toEqual(expect.any(String));
    expect(ctx.locals.cspNonce ?? "").not.toHaveLength(0);
  });

  it("301s www to the apex host, preserving path", async () => {
    const ctx = buildContext("https://www.tone.rip/projects");
    const res = await run(ctx);
    expect(res.status).toBe(301);
    expect(res.headers.get("Location")).toBe("https://tone.rip/projects");
  });

  it("redirects a trailing-slash URL to the canonical form", async () => {
    // `trailingSlash: "never"` alone answers /cv/ with a 404, which is worse
    // than the duplicate content it fixes - the old sitemap published exactly
    // that form.
    const ctx = buildContext("https://tone.rip/cv/?x=1");
    const res = await run(ctx);
    expect(res.status).toBe(308);
    expect(res.headers.get("Location")).toBe("https://tone.rip/cv?x=1");
  });

  it("leaves the root alone", async () => {
    const ctx = buildContext("https://tone.rip/");
    const res = await run(ctx);
    expect(res.status).not.toBe(308);
  });

  it("upgrades a plaintext www request to https rather than mirroring it", async () => {
    // The redirect used to copy the request URL and change only the host, so
    // http://www → http://apex: a second plaintext hop, taken before HSTS is
    // ever set on the apex, which is exactly when it matters most.
    const ctx = buildContext("http://www.tone.rip/projects?a=1");
    const res = await run(ctx);
    expect(res.status).toBe(301);
    expect(res.headers.get("Location")).toBe("https://tone.rip/projects?a=1");
  });

  it("serves a blanket-disallow robots.txt on the dev host", async () => {
    const ctx = buildContext("https://dev.tone.rip/robots.txt");
    const res = await run(ctx);
    expect(await res.text()).toBe("User-agent: *\nDisallow: /\n");
    expect(res.headers.get("X-Robots-Tag")).toContain("noindex");
  });

  it("serves the RFC 9727 API catalog pointing at api.tone.rip/projects", async () => {
    const ctx = buildContext("https://tone.rip/.well-known/api-catalog");
    const res = await run(ctx);
    const body = (await res.json()) as { linkset: { anchor: string }[] };
    expect(body.linkset[0]?.anchor).toBe("https://api.tone.rip/projects");
    expect(res.headers.get("Content-Type")).toContain(
      "application/linkset+json",
    );
  });

  it("serves the machine-readable markdown homepage for Accept: text/markdown", async () => {
    const ctx = buildContext("https://tone.rip/", {
      headers: { Accept: "text/markdown" },
    });
    const res = await run(ctx);
    expect(res.headers.get("Content-Type")).toContain("text/markdown");
    const body = await res.text();
    expect(body).toContain("# tone");
  });

  it("passes browser requests for / through to the app", async () => {
    const ctx = buildContext("https://tone.rip/", {
      headers: { Accept: "text/html" },
    });
    const res = await run(ctx);
    expect(await res.text()).toBe("<html><body>ok</body></html>");
  });

  it("sets security headers and a CSP pointing report-uri at api.tone.rip/csp-report", async () => {
    const ctx = buildContext("https://tone.rip/");
    const res = await run(ctx);
    expect(res.headers.get("X-Content-Type-Options")).toBe("nosniff");
    expect(res.headers.get("X-Frame-Options")).toBe("SAMEORIGIN");
    const csp = res.headers.get("Content-Security-Policy") ?? "";
    expect(csp).toContain("report-uri https://api.tone.rip/csp-report");
    expect(csp).toContain(`'nonce-${ctx.locals.cspNonce}'`);
  });

  it("relaxes script-src/style-src to unsafe-inline on localhost", async () => {
    const ctx = buildContext("http://localhost:4321/");
    const res = await run(ctx);
    const csp = res.headers.get("Content-Security-Policy") ?? "";
    expect(csp).toContain("script-src 'self' 'unsafe-inline'");
  });

  it("tags dev-host responses with X-Robots-Tag", async () => {
    const ctx = buildContext("https://dev.tone.rip/anything");
    const res = await run(ctx);
    expect(res.headers.get("X-Robots-Tag")).toContain("noindex");
  });
});
