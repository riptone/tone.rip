import { env, SELF } from "cloudflare:test";
import { beforeEach, describe, expect, it } from "vitest";

const FP_ALLOWED = `SHA256:${"A".repeat(43)}`;
const FP_KNOWN_NO_SCOPES = `SHA256:${"B".repeat(43)}`;
const FP_UNKNOWN = `SHA256:${"C".repeat(43)}`;
const TOKEN = "test-gateway-token";

type MutableEnv = {
  SSH_GATEWAY_TOKEN?: string;
  SSH_AUTHORIZED_KEYS?: string;
};

function configure(overrides: MutableEnv = {}) {
  const target = env as unknown as MutableEnv;
  target.SSH_GATEWAY_TOKEN = TOKEN;
  target.SSH_AUTHORIZED_KEYS = [
    `${FP_ALLOWED} laptop dotfiles`,
    `${FP_KNOWN_NO_SCOPES} phone`,
  ].join("\n");
  Object.assign(target, overrides);
}

function authorize(fingerprint: unknown, token: string | null = TOKEN) {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (token !== null) headers.Authorization = `Bearer ${token}`;
  return SELF.fetch("https://api.tone.rip/ssh/authorize", {
    method: "POST",
    headers,
    body: JSON.stringify({ fingerprint }),
  });
}

describe("POST /ssh/authorize", () => {
  beforeEach(() => {
    configure();
  });

  it("grants the scopes an allowlisted key holds", async () => {
    const res = await authorize(FP_ALLOWED);
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({
      allowed: true,
      label: "laptop",
      scopes: ["dotfiles"],
    });
  });

  it("allows a known key while granting it nothing", async () => {
    const res = await authorize(FP_KNOWN_NO_SCOPES);
    expect(await res.json()).toEqual({
      allowed: true,
      label: "phone",
      scopes: [],
    });
  });

  it("denies an unknown key", async () => {
    const res = await authorize(FP_UNKNOWN);
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ allowed: false, label: "", scopes: [] });
  });

  it("answers unknown keys in the same shape as known ones", async () => {
    // The response body must not be a side channel for "does this
    // fingerprint exist" - only the values differ, never the keys.
    const known = (await (await authorize(FP_ALLOWED)).json()) as object;
    const unknown = (await (await authorize(FP_UNKNOWN)).json()) as object;
    expect(Object.keys(known).sort()).toEqual(Object.keys(unknown).sort());
  });

  it("never lets the answer be cached", async () => {
    const res = await authorize(FP_ALLOWED);
    expect(res.headers.get("Cache-Control")).toBe("no-store");
  });

  it("rejects a missing or wrong bearer token", async () => {
    for (const token of [null, "", "wrong-token", `${TOKEN}x`]) {
      const res = await authorize(FP_ALLOWED, token);
      expect(res.status).toBe(401);
      expect(res.headers.get("Content-Type")).toContain(
        "application/problem+json",
      );
    }
  });

  it("fails closed when no gateway token is configured", async () => {
    // A half-configured deploy must refuse everything rather than fall back
    // to an open endpoint.
    configure({ SSH_GATEWAY_TOKEN: undefined });
    expect((await authorize(FP_ALLOWED)).status).toBe(401);
    expect((await authorize(FP_ALLOWED, "")).status).toBe(401);
  });

  it("denies everything when the allowlist is unset", async () => {
    configure({ SSH_AUTHORIZED_KEYS: undefined });
    expect(await (await authorize(FP_ALLOWED)).json()).toEqual({
      allowed: false,
      label: "",
      scopes: [],
    });
  });

  it("rejects a malformed fingerprint as a validation problem", async () => {
    for (const bad of ["", "not-a-fingerprint", "SHA256:short", 42, null]) {
      const res = await authorize(bad);
      expect(res.status).toBe(400);
      expect(res.headers.get("Content-Type")).toContain(
        "application/problem+json",
      );
    }
  });

  it("checks the token before it looks at the body", async () => {
    // Otherwise a malformed-body 400 versus an auth 401 tells an
    // unauthenticated caller that the endpoint exists and is configured.
    const res = await SELF.fetch("https://api.tone.rip/ssh/authorize", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{not json",
    });
    expect(res.status).toBe(401);
  });

  it("rejects an oversized body", async () => {
    const res = await SELF.fetch("https://api.tone.rip/ssh/authorize", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${TOKEN}`,
      },
      body: JSON.stringify({
        fingerprint: FP_ALLOWED,
        padding: "x".repeat(4096),
      }),
    });
    expect(res.status).toBe(413);
  });

  it("does not answer GET", async () => {
    const res = await SELF.fetch("https://api.tone.rip/ssh/authorize");
    expect(res.status).toBe(404);
  });
});
