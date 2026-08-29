import { afterEach, describe, expect, it, vi } from "vitest";
import { fetchWithTimeout, withTimeout } from "../src/index";

describe("fetchWithTimeout", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("resolves with the response when it arrives before the timeout", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("ok", { status: 200 })),
    );
    const res = await fetchWithTimeout("https://example.com", 1000);
    expect(res.status).toBe(200);
  });

  it("passes through init options alongside the abort signal", async () => {
    const fetchMock = vi.fn(
      async (_input: string | URL, _init?: RequestInit) => new Response("ok"),
    );
    vi.stubGlobal("fetch", fetchMock);
    await fetchWithTimeout("https://example.com", 1000, { cache: "no-store" });
    const call = fetchMock.mock.calls[0];
    expect(call).toBeDefined();
    const init = call?.[1];
    expect(init?.cache).toBe("no-store");
    expect(init?.signal).toBeInstanceOf(AbortSignal);
  });

  it("aborts and rejects once the timeout elapses", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((_url: string, init: RequestInit) => {
        return new Promise((_resolve, reject) => {
          init.signal?.addEventListener("abort", () =>
            reject(new Error("aborted")),
          );
        });
      }),
    );
    await expect(fetchWithTimeout("https://example.com", 5)).rejects.toThrow();
  });
});

/* `withTimeout` exists so a caller that does not reach the network through
   global `fetch` still gets a deadline - apps/dashboard talks to apps/api
   over a Cloudflare service binding, whose `.fetch` is a method on a binding
   object. These cover it directly rather than only through the wrapper. */
describe("withTimeout", () => {
  it("resolves with whatever the attempt returns", async () => {
    const value = await withTimeout(1000, async () => "done");
    expect(value).toBe("done");
  });

  it("hands the attempt a signal that is not yet aborted", async () => {
    const seen = await withTimeout(1000, async (signal) => signal.aborted);
    expect(seen).toBe(false);
  });

  it("aborts the signal once the timeout elapses", async () => {
    await expect(
      withTimeout(
        5,
        (signal) =>
          new Promise((_resolve, reject) => {
            signal.addEventListener("abort", () =>
              reject(new Error("aborted")),
            );
          }),
      ),
    ).rejects.toThrow("aborted");
  });

  it("clears the timer when the attempt throws, not only when it resolves", async () => {
    const clear = vi.spyOn(globalThis, "clearTimeout");
    await expect(
      withTimeout(1000, async () => {
        throw new Error("boom");
      }),
    ).rejects.toThrow("boom");
    expect(clear).toHaveBeenCalled();
    clear.mockRestore();
  });
});
