import { afterEach, describe, expect, it, vi } from "vitest";
import { pingUrl } from "../src/scripts/client-probe";

// Which path to probe is no longer derived here, or anywhere in this repo:
// it arrives per app from the API, which reads the exceptions from a secret.
// The hostnames that need one are not this repository's to publish.

describe("pingUrl", () => {
  const originalFetch = globalThis.fetch;

  afterEach(() => {
    vi.unstubAllGlobals();
    globalThis.fetch = originalFetch;
  });

  it("returns false for an unparseable href instead of throwing", async () => {
    expect(await pingUrl("not a url", 100)).toBe(false);
  });

  it("returns true when the no-cors fetch resolves", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response()),
    );
    expect(await pingUrl("https://example.com", 100)).toBe(true);
  });

  it("probes the path it is given", async () => {
    // Typed parameter, not `async () => ...`: without it the mock's `calls`
    // is the empty tuple and indexing it is a type error that vitest happily
    // runs and `astro check` refuses.
    const fetchMock = vi.fn(
      async (_input: string | URL | Request) => new Response(),
    );
    vi.stubGlobal("fetch", fetchMock);
    await pingUrl("https://app.example.com", 100, "/favicon.ico");
    expect(new URL(String(fetchMock.mock.calls[0]?.[0])).pathname).toBe(
      "/favicon.ico",
    );
  });

  it("returns false when the fetch throws", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("network down");
      }),
    );
    expect(await pingUrl("https://example.com", 100)).toBe(false);
  });
});
