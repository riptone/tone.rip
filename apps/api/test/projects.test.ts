import { SELF } from "cloudflare:test";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const GITHUB_REPOS = [
  {
    name: "tone.rip",
    html_url: "https://github.com/riptone/tone.rip",
    stargazers_count: 5,
    updated_at: "2026-01-01T00:00:00Z",
  },
];

describe("GET /projects", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () => new Response(JSON.stringify(GITHUB_REPOS), { status: 200 }),
      ),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    globalThis.fetch = originalFetch;
  });

  it("proxies and simplifies the GitHub repo list", async () => {
    const res = await SELF.fetch("https://api.tone.rip/projects");
    expect(res.status).toBe(200);
    const body = (await res.json()) as Array<{ name: string }>;
    expect(body).toEqual([
      expect.objectContaining({ name: "tone.rip", stars: 5, forks: 0 }),
    ]);
  });

  it("sets Access-Control-Allow-Origin for a trusted cross-origin caller", async () => {
    const res = await SELF.fetch("https://api.tone.rip/projects", {
      headers: { Origin: "https://tone.rip" },
    });
    expect(res.status).toBe(200);
    expect(res.headers.get("Access-Control-Allow-Origin")).toBe(
      "https://tone.rip",
    );
  });

  it("omits Access-Control-Allow-Origin for an untrusted origin", async () => {
    const res = await SELF.fetch("https://api.tone.rip/projects", {
      headers: { Origin: "https://evil.example.com" },
    });
    // The global cors() middleware doesn't reject the request outright - it
    // just withholds the header, which is what makes the browser (not this
    // server) block the response from being read cross-origin.
    expect(res.status).toBe(200);
    expect(res.headers.get("Access-Control-Allow-Origin")).toBeNull();
  });

  it("sets Cross-Origin-Resource-Policy: cross-origin (this API is deliberately multi-origin)", async () => {
    const res = await SELF.fetch("https://api.tone.rip/projects");
    expect(res.headers.get("Cross-Origin-Resource-Policy")).toBe(
      "cross-origin",
    );
  });
});

describe("GET /projects/:name/readme", () => {
  const originalFetch = globalThis.fetch;

  afterEach(() => {
    vi.unstubAllGlobals();
    globalThis.fetch = originalFetch;
  });

  /**
   * Stubs GitHub for both the repo list and the readme, then force-refreshes
   * the cached repo list so the known-repo check has data regardless of what
   * other tests left in the shared edge cache.
   */
  async function stubGitHubAndPrime(
    readme: () => Response,
    repos: unknown[] = GITHUB_REPOS,
  ) {
    const fetchMock = vi.fn(async (input: string | URL | Request) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.endsWith("/readme")) return readme();
      return new Response(JSON.stringify(repos), { status: 200 });
    });
    vi.stubGlobal("fetch", fetchMock);
    await SELF.fetch("https://api.tone.rip/projects", {
      headers: { "x-tone-revalidate": "1" },
    });
    fetchMock.mockClear();
    return fetchMock;
  }

  const okReadme = () => new Response("<h1>hi</h1>", { status: 200 });

  /** A repo the edge cache hasn't seen a README for yet, so each test's
   *  upstream behaviour is what's actually being asserted. */
  const repo = (name: string) => [
    {
      name,
      html_url: `https://github.com/riptone/${name}`,
      stargazers_count: 0,
      updated_at: "2026-01-01T00:00:00Z",
    },
  ];

  it("returns the rendered README html for a known repo", async () => {
    await stubGitHubAndPrime(okReadme);
    const res = await SELF.fetch(
      "https://api.tone.rip/projects/tone.rip/readme",
    );
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ html: "<h1>hi</h1>" });
  });

  it("proxies to GitHub's readme endpoint for that repo", async () => {
    const fetchMock = await stubGitHubAndPrime(okReadme);
    await SELF.fetch("https://api.tone.rip/projects/tone.rip/readme");

    const urls = fetchMock.mock.calls.map((call) => String(call[0]));
    expect(urls).toContain(
      "https://api.github.com/repos/riptone/tone.rip/readme",
    );
  });

  it("never calls GitHub for a repo that isn't in the projects list", async () => {
    // Otherwise every made-up name is an uncacheable upstream request, and a
    // loop of them drains the token's hourly budget.
    const fetchMock = await stubGitHubAndPrime(okReadme);

    const res = await SELF.fetch(
      "https://api.tone.rip/projects/not-a-real-repo/readme",
    );
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ html: null });

    const readmeCalls = fetchMock.mock.calls.filter((call) =>
      String(call[0]).endsWith("/readme"),
    );
    expect(readmeCalls).toHaveLength(0);
  });

  it("returns html:null (not an error) when a known repo has no README", async () => {
    await stubGitHubAndPrime(
      () => new Response("", { status: 404 }),
      repo("bare-repo"),
    );
    const res = await SELF.fetch(
      "https://api.tone.rip/projects/bare-repo/readme",
    );
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ html: null });
  });

  it("returns html:null when GitHub errors and nothing is cached", async () => {
    await stubGitHubAndPrime(
      () => new Response("", { status: 504 }),
      repo("flaky-repo"),
    );
    const res = await SELF.fetch(
      "https://api.tone.rip/projects/flaky-repo/readme",
    );
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ html: null });
  });

  it("rejects a repo name that isn't a valid GitHub repo name", async () => {
    const fetchMock = await stubGitHubAndPrime(okReadme);
    const res = await SELF.fetch(
      "https://api.tone.rip/projects/..%2F..%2Fetc/readme",
    );
    expect(res.status).toBe(400);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("API basics", () => {
  it("renders a 404 as application/problem+json, like every other error", async () => {
    const res = await SELF.fetch("https://api.tone.rip/does-not-exist");
    expect(res.status).toBe(404);
    expect(res.headers.get("Content-Type")).toBe(
      "application/problem+json; charset=utf-8",
    );
    expect(await res.json()).toMatchObject({
      status: 404,
      title: "Not Found",
      instance: "/does-not-exist",
    });
  });

  it("describes itself at the root instead of 404ing", async () => {
    const res = await SELF.fetch("https://api.tone.rip/");
    expect(res.status).toBe(200);
    const body = (await res.json()) as { endpoints: Array<{ href: string }> };
    expect(body.endpoints.length).toBeGreaterThan(0);
  });

  it("only advertises methods that actually exist on preflight", async () => {
    const res = await SELF.fetch("https://api.tone.rip/projects", {
      method: "OPTIONS",
      headers: {
        Origin: "https://tone.rip",
        "Access-Control-Request-Method": "GET",
      },
    });
    const allowed = res.headers.get("Access-Control-Allow-Methods") ?? "";
    expect(allowed).not.toMatch(/DELETE|PATCH|PUT/);
  });
});
