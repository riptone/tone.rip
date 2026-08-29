import { afterEach, describe, expect, it, vi } from "vitest";
import { pingUrl } from "../src/scripts/client-probe";

// The Vaultwarden-favicon path resolution pingUrl relies on moved to
// @repo/content's resolveProbePath - see packages/content/src/app-probe.ts.

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
