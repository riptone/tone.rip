import { SELF } from "cloudflare:test";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ACCESS_AUD,
  ACCESS_HEADER,
  ACCESS_ISSUER,
  ACCESS_JWKS_URL,
  generateSignedAccessToken,
} from "./support/access-jwt";

describe("GET /secrets", () => {
  const originalFetch = globalThis.fetch;
  afterEach(() => {
    vi.unstubAllGlobals();
    globalThis.fetch = originalFetch;
  });

  it("rejects requests without the Access JWT", async () => {
    const res = await SELF.fetch("https://api.example.com/secrets");
    expect(res.status).toBe(401);
  });

  it("answers names-only JSON, uncached, given a valid Access JWT", async () => {
    const now = Math.floor(Date.now() / 1000);
    const { token, jwks } = await generateSignedAccessToken({
      iss: ACCESS_ISSUER,
      aud: ACCESS_AUD,
      iat: now,
      exp: now + 3600,
    });
    // Everything but the JWKS fails, so the answer is "unconfigured" or
    // "unavailable" whatever a local .dev.vars holds - never a real vault.
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request) =>
        String(input instanceof Request ? input.url : input) === ACCESS_JWKS_URL
          ? Response.json(jwks)
          : new Response(null, { status: 503 }),
      ),
    );
    const res = await SELF.fetch("https://api.example.com/secrets", {
      headers: { [ACCESS_HEADER]: token },
    });
    expect(res.status).toBe(200);
    expect(res.headers.get("cache-control")).toBe("no-store");
    const body = (await res.json()) as { secrets: unknown[]; state: string };
    expect(body.secrets).toEqual([]);
    expect(["unconfigured", "unavailable"]).toContain(body.state);
  });

  it("is not advertised in the API catalog", async () => {
    const res = await SELF.fetch("https://api.example.com/");
    expect(await res.text()).not.toContain("/secrets");
  });
});
