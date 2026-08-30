import { afterEach, describe, expect, it } from "vitest";
import { createSiteApp } from "../src/fetch";

/**
 * The pipeline with its two production defaults swapped out.
 *
 * `render` stands in for Astro's own dispatch, which needs a built Worker's
 * ambient manifest; `bindNonce` stands in for the write to `Astro.locals`,
 * which needs the same. Everything between those two - the redirects, the
 * short-circuits, the header set and the order they run in - is the real
 * thing, which is what these tests are for.
 */
function buildApp() {
  let nonce: string | undefined;
  const app = createSiteApp({
    bindNonce: (_c, value) => {
      nonce = value;
    },
    render: async (c) => c.html("<html><body>ok</body></html>"),
  });
  return { app, nonce: () => nonce };
}

const get = (url: string, init?: RequestInit) =>
  buildApp().app.request(url, { redirect: "manual", ...init });

describe("apps/web request pipeline", () => {
  it("publishes a cspNonce to Astro locals", async () => {
    const { app, nonce } = buildApp();
    await app.request("https://tone.rip/", { redirect: "manual" });
    expect(nonce()).toEqual(expect.any(String));
    expect(nonce() ?? "").not.toHaveLength(0);
  });

  it("301s www to the apex host, preserving path", async () => {
    const res = await get("https://www.tone.rip/projects");
    expect(res.status).toBe(301);
    expect(res.headers.get("Location")).toBe("https://tone.rip/projects");
  });

  it("upgrades a plaintext www request to https rather than mirroring it", async () => {
    // The redirect used to copy the request URL and change only the host, so
    // http://www -> http://apex: a second plaintext hop, taken before HSTS is
    // ever set on the apex, which is exactly when it matters most.
    const res = await get("http://www.tone.rip/projects?a=1");
    expect(res.status).toBe(301);
    expect(res.headers.get("Location")).toBe("https://tone.rip/projects?a=1");
  });

  it("redirects a trailing-slash URL to the canonical form", async () => {
    // `trailingSlash: "never"` alone answers /cv/ with a 404, which is worse
    // than the duplicate content it fixes - the old sitemap published exactly
    // that form. Astro's own trailingSlash() handler is a no-op under
    // "ignore", which is why this rule is still ours.
    const res = await get("https://tone.rip/cv/?x=1");
    expect(res.status).toBe(308);
    expect(res.headers.get("Location")).toBe("https://tone.rip/cv?x=1");
  });

  it("leaves the root alone", async () => {
    const res = await get("https://tone.rip/");
    expect(res.status).not.toBe(308);
  });

  it("serves a blanket-disallow robots.txt on the dev host", async () => {
    const res = await get("https://dev.tone.rip/robots.txt");
    expect(await res.text()).toBe("User-agent: *\nDisallow: /\n");
    expect(res.headers.get("X-Robots-Tag")).toContain("noindex");
  });

  it("tags dev-host responses with X-Robots-Tag", async () => {
    const res = await get("https://dev.tone.rip/anything");
    expect(res.headers.get("X-Robots-Tag")).toContain("noindex");
  });

  it("serves the RFC 9727 API catalog pointing at api.tone.rip/projects", async () => {
    const res = await get("https://tone.rip/.well-known/api-catalog");
    const body = (await res.json()) as { linkset: { anchor: string }[] };
    expect(body.linkset[0]?.anchor).toBe("https://api.tone.rip/projects");
    expect(res.headers.get("Content-Type")).toContain(
      "application/linkset+json",
    );
  });

  it("serves the machine-readable markdown homepage for Accept: text/markdown", async () => {
    const res = await get("https://tone.rip/", {
      headers: { Accept: "text/markdown" },
    });
    expect(res.headers.get("Content-Type")).toContain("text/markdown");
    expect(await res.text()).toContain("# tone");
  });

  it("passes browser requests for / through to the renderer", async () => {
    const res = await get("https://tone.rip/", {
      headers: { Accept: "text/html" },
    });
    expect(await res.text()).toBe("<html><body>ok</body></html>");
  });

  it("sets security headers and a CSP pointing report-uri at api.tone.rip/csp-report", async () => {
    const { app, nonce } = buildApp();
    const res = await app.request("https://tone.rip/", { redirect: "manual" });
    expect(res.headers.get("X-Content-Type-Options")).toBe("nosniff");
    expect(res.headers.get("X-Frame-Options")).toBe("SAMEORIGIN");
    const csp = res.headers.get("Content-Security-Policy") ?? "";
    expect(csp).toContain("report-uri https://api.tone.rip/csp-report");
    expect(csp).toContain(`'nonce-${nonce()}'`);
  });

  it("advertises the api-catalog and llms.txt in the Link header", async () => {
    const res = await get("https://tone.rip/");
    const link = res.headers.get("Link") ?? "";
    expect(link).toContain('rel="api-catalog"');
    expect(link).toContain("llms.txt");
  });

  it("forces private no-cache on HTML, which carries a per-request nonce", async () => {
    const res = await get("https://tone.rip/");
    expect(res.headers.get("Cache-Control")).toBe(
      "private, max-age=0, must-revalidate",
    );
  });

  it("relaxes script-src/style-src to unsafe-inline on localhost", async () => {
    const res = await get("http://localhost:4321/");
    const csp = res.headers.get("Content-Security-Policy") ?? "";
    expect(csp).toContain("script-src 'self' 'unsafe-inline'");
  });
});

/**
 * A stand-in for the Workers HTMLRewriter, which does not exist under node.
 *
 * The nonce backstop is the one piece of this pipeline that never ran in a
 * test before - `nonceInlineTags` returns early when the global is missing,
 * so every previous run took the early exit and the stamping logic below was
 * asserted by nobody. This records the selectors it was asked about and
 * exposes the handler so the `:not([nonce])` workaround can be exercised
 * directly.
 */
class FakeElement {
  constructor(private readonly attrs: Record<string, string>) {}
  getAttribute(name: string): string | null {
    return this.attrs[name] ?? null;
  }
  setAttribute(name: string, value: string): void {
    this.attrs[name] = value;
  }
  get(name: string): string | undefined {
    return this.attrs[name];
  }
}

interface Stamp {
  element(el: FakeElement): void;
}

class FakeRewriter {
  static last: FakeRewriter | undefined;
  readonly selectors: string[] = [];
  stamp: Stamp | undefined;
  constructor() {
    FakeRewriter.last = this;
  }
  on(selector: string, stamp: Stamp): this {
    this.selectors.push(selector);
    this.stamp = stamp;
    return this;
  }
  transform(response: Response): Response {
    return new Response("rewritten", response);
  }
}

describe("the inline-nonce backstop", () => {
  afterEach(() => {
    Reflect.deleteProperty(globalThis, "HTMLRewriter");
    FakeRewriter.last = undefined;
  });

  it("runs over HTML responses and asks about both inline tag types", async () => {
    Reflect.set(globalThis, "HTMLRewriter", FakeRewriter);
    const res = await get("https://tone.rip/");
    expect(await res.text()).toBe("rewritten");
    expect(FakeRewriter.last?.selectors).toEqual(["style", "script"]);
  });

  it("stamps a tag that has no nonce and leaves an existing one alone", async () => {
    Reflect.set(globalThis, "HTMLRewriter", FakeRewriter);
    const { app, nonce } = buildApp();
    await app.request("https://tone.rip/", { redirect: "manual" });

    const stamp = FakeRewriter.last?.stamp;
    const bare = new FakeElement({});
    const alreadyNonced = new FakeElement({ nonce: "theirs" });
    stamp?.element(bare);
    stamp?.element(alreadyNonced);

    expect(bare.get("nonce")).toBe(nonce());
    expect(alreadyNonced.get("nonce")).toBe("theirs");
  });

  it("leaves non-HTML responses untransformed", async () => {
    Reflect.set(globalThis, "HTMLRewriter", FakeRewriter);
    // The catalog answers before the renderer, as JSON.
    const res = await get("https://tone.rip/.well-known/api-catalog");
    expect(await res.text()).not.toBe("rewritten");
  });
});
