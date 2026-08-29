import { SELF } from "cloudflare:test";
import { sign } from "hono/jwt";
import { afterEach, describe, expect, it, vi } from "vitest";

const ACCESS_TEAM_DOMAIN = "riptone.cloudflareaccess.com";
const ACCESS_ISSUER = `https://${ACCESS_TEAM_DOMAIN}`;
const ACCESS_AUD =
  "28a3efd8f96a2e859f3bcd8158570e67538c297b25a8d7de9803b877e8a1881a";
const ACCESS_JWKS_URL = `${ACCESS_ISSUER}/cdn-cgi/access/certs`;
const ACCESS_HEADER = "Cf-Access-Jwt-Assertion";
const KID = "test-kid";

async function generateSignedAccessToken(payload: Record<string, unknown>) {
  const keyPair = (await crypto.subtle.generateKey(
    {
      name: "RSASSA-PKCS1-v1_5",
      hash: "SHA-256",
      modulusLength: 2048,
      publicExponent: new Uint8Array([1, 0, 1]),
    },
    true,
    ["sign", "verify"],
  )) as CryptoKeyPair;
  const privateJwk = (await crypto.subtle.exportKey(
    "jwk",
    keyPair.privateKey,
  )) as JsonWebKey;
  const publicJwk = (await crypto.subtle.exportKey(
    "jwk",
    keyPair.publicKey,
  )) as JsonWebKey;

  const token = await sign(
    payload,
    { ...privateJwk, alg: "RS256", kid: KID },
    "RS256",
  );
  const jwks = { keys: [{ ...publicJwk, alg: "RS256", kid: KID, use: "sig" }] };
  return { token, jwks };
}

describe("GET /status", () => {
  const originalFetch = globalThis.fetch;

  afterEach(() => {
    vi.unstubAllGlobals();
    globalThis.fetch = originalFetch;
  });

  it("rejects requests missing the Access JWT header", async () => {
    const res = await SELF.fetch("https://api.tone.rip/status");
    expect(res.status).toBe(401);
  });

  it("rejects requests bearing an invalid Access JWT", async () => {
    const res = await SELF.fetch("https://api.tone.rip/status", {
      headers: { [ACCESS_HEADER]: "not-a-real-token" },
    });
    expect(res.status).toBe(401);
  });

  it("probes public apps but reports tailnet-only ones as unknown, given a valid Access JWT", async () => {
    const now = Math.floor(Date.now() / 1000);
    const { token, jwks } = await generateSignedAccessToken({
      iss: ACCESS_ISSUER,
      aud: ACCESS_AUD,
      iat: now,
      exp: now + 3600,
    });

    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string | URL | Request) => {
        const url = typeof input === "string" ? input : input.toString();
        if (url === ACCESS_JWKS_URL) {
          return new Response(JSON.stringify(jwks), { status: 200 });
        }
        return new Response(null, { status: 200 });
      }),
    );

    const res = await SELF.fetch("https://api.tone.rip/status", {
      headers: { [ACCESS_HEADER]: token },
    });
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      apps: Array<{ name: string; status: string }>;
      tailnet: { device: unknown };
    };
    // The list itself is no longer this route's to produce: it comes from
    // Cloudflare Access (services/access-apps.ts), which without a token in
    // the test environment answers with nothing. So this asserts the contract
    // - authenticated, right shape - and not a count that now depends on an
    // external account.
    //
    // The two halves it used to cover live where they can be tested against
    // fixtures instead of real data:
    //   - Access -> tile mapping: apps.test.ts
    //   - which hosts get probed:  app-health.test.ts
    expect(Array.isArray(body.apps)).toBe(true);
    expect(body).toHaveProperty("tailnet");

    // Whatever is in the list, a tailnet-only host is never given a verdict:
    // Cloudflare's edge cannot route to a CGNAT address, so a probe result
    // would be fiction. The browser's own ping decides those tiles instead
    // (apps/dashboard's client-probe.ts).
    expect(body.apps.every((app) => app.status === "unknown")).toBe(true);
    // No TAILSCALE_* vars are configured in the test environment, so the
    // tailnet lookup should short-circuit to null without an extra fetch.
    expect(body.tailnet.device).toBeNull();
  });
});
