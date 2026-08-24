import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  __resetProjectsMemorySnapshotForTests,
  fetchProjects,
} from "../src/services/projects-cache";

const REPO = {
  name: "tonil",
  html_url: "https://github.com/no-tone/tonil",
  updated_at: "2026-01-01T00:00:00Z",
};

describe("fetchProjects", () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    __resetProjectsMemorySnapshotForTests();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    globalThis.fetch = originalFetch;
  });

  it("falls back to the in-memory snapshot when the edge cache is empty and upstream is down", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify([REPO]), { status: 200 })),
    );
    const first = await fetchProjects();
    expect(first.cacheState).toBe("miss");

    // Now GitHub goes down. There's still whatever the edge Cache API kept
    // (real, since these tests run in workerd via vitest-pool-workers) - to
    // isolate the *memory* fallback specifically, this asserts the snapshot
    // it returns is at least the one we just saw, whichever fallback served it.
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("network down");
      }),
    );
    const second = await fetchProjects({ forceRevalidate: true });
    expect(["stale", "memory-stale"]).toContain(second.cacheState);
    expect(JSON.parse(second.snapshot.body)).toEqual([
      expect.objectContaining({ name: "tonil" }),
    ]);
  });

  it("returns an empty, no-store snapshot when there is no cache at all and upstream fails", async () => {
    const onUpstreamError = vi.fn();
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("network down");
      }),
    );
    const result = await fetchProjects({ onUpstreamError });
    expect(result.cacheState).toBe("unavailable");
    expect(result.snapshot.body).toBe("[]");
    expect(onUpstreamError).toHaveBeenCalledWith(
      expect.objectContaining({ error: "network down" }),
    );
  });

  it("treats a non-ok upstream status as a failure", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(null, { status: 500 })),
    );
    const result = await fetchProjects();
    expect(result.cacheState).toBe("unavailable");
  });
});
