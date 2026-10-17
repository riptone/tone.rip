import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  accessTokenKey,
  bytesToBase64,
  parseAccessToken,
} from "../src/services/bitwarden-crypto";
import {
  __resetBitwardenForTests,
  fetchSecretNames,
} from "../src/services/bitwarden-secrets";

const utf8 = new TextEncoder();
const ORG = "4d1e8f0a-0000-4000-8000-000000000001";
const PROJECT = "4d1e8f0a-0000-4000-8000-000000000002";
const TOKEN = `0.ec2c1d46-6a4b-4751-a310-af9601317f2d.client-secret:${bytesToBase64(new Uint8Array(16).fill(3))}`;

async function encString(text: string, key: Uint8Array<ArrayBuffer>) {
  const iv = crypto.getRandomValues(new Uint8Array(16));
  const enc = await crypto.subtle.importKey(
    "raw",
    key.slice(0, 32),
    { name: "AES-CBC" },
    false,
    ["encrypt"],
  );
  const data = new Uint8Array(
    await crypto.subtle.encrypt(
      { name: "AES-CBC", iv },
      enc,
      utf8.encode(text),
    ),
  );
  const mk = await crypto.subtle.importKey(
    "raw",
    key.slice(32),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const mac = new Uint8Array(
    await crypto.subtle.sign("HMAC", mk, new Uint8Array([...iv, ...data])),
  );
  return `2.${bytesToBase64(iv)}|${bytesToBase64(data)}|${bytesToBase64(mac)}`;
}

const b64url = (o: object) => btoa(JSON.stringify(o)).replaceAll("=", "");

/* A fake Bitwarden: the identity endpoint hands out the organization key
   wrapped with the token's derived key, the API lists encrypted names. */
async function fakeBitwarden(calls: string[]) {
  const orgKey = crypto.getRandomValues(new Uint8Array(64));
  const payload = await encString(
    JSON.stringify({ encryptionKey: bytesToBase64(orgKey) }),
    await accessTokenKey(parseAccessToken(TOKEN)),
  );
  const list = {
    secrets: [
      {
        id: "4d1e8f0a-0000-4000-8000-000000000010",
        key: await encString("STRIPE_WEBHOOK_SECRET", orgKey),
        creationDate: "2026-10-01T00:00:00Z",
        revisionDate: "2026-10-12T00:00:00Z",
        projects: [{ id: PROJECT }],
      },
      {
        id: "4d1e8f0a-0000-4000-8000-000000000011",
        key: await encString("API_KEY_N8N", orgKey),
        creationDate: "2026-10-01T00:00:00Z",
        revisionDate: "2026-10-02T00:00:00Z",
        projects: [],
      },
    ],
    projects: [{ id: PROJECT, name: await encString("rittea", orgKey) }],
  };
  return vi.fn(async (input: string | URL | Request) => {
    const url = String(input instanceof Request ? input.url : input);
    calls.push(url);
    if (url.endsWith("/connect/token"))
      return Response.json({
        access_token: `h.${b64url({ organization: ORG })}.s`,
        expires_in: 3600,
        encrypted_payload: payload,
      });
    if (url.endsWith(`/organizations/${ORG}/secrets`))
      return Response.json(list);
    return new Response("not here", { status: 404 });
  });
}

describe("fetchSecretNames", () => {
  const originalFetch = globalThis.fetch;
  beforeEach(() => __resetBitwardenForTests());
  afterEach(() => {
    vi.unstubAllGlobals();
    globalThis.fetch = originalFetch;
  });

  it("is unconfigured without a token, and calls nothing", async () => {
    const calls: string[] = [];
    vi.stubGlobal("fetch", await fakeBitwarden(calls));
    expect(
      await fetchSecretNames({ accessToken: undefined, region: "eu" }),
    ).toEqual({ state: "unconfigured", secrets: [] });
    expect(calls).toEqual([]);
  });

  it("decrypts names and projects, sorted, from the EU region by default", async () => {
    const calls: string[] = [];
    vi.stubGlobal("fetch", await fakeBitwarden(calls));
    const r = await fetchSecretNames({
      accessToken: TOKEN,
      region: undefined,
      now: 1,
    });
    expect(r).toEqual({
      state: "ok",
      secrets: [
        {
          name: "API_KEY_N8N",
          projects: [],
          revisedAt: "2026-10-02T00:00:00Z",
        },
        {
          name: "STRIPE_WEBHOOK_SECRET",
          projects: ["rittea"],
          revisedAt: "2026-10-12T00:00:00Z",
        },
      ],
    });
    expect(calls).toEqual([
      "https://identity.bitwarden.eu/connect/token",
      `https://api.bitwarden.eu/organizations/${ORG}/secrets`,
    ]);
  });

  it("never asks for a secret's value", async () => {
    const calls: string[] = [];
    vi.stubGlobal("fetch", await fakeBitwarden(calls));
    await fetchSecretNames({ accessToken: TOKEN, region: "eu", now: 1 });
    expect(calls.some((u) => /\/secrets\/[0-9a-f-]+$|get-by-ids/.test(u))).toBe(
      false,
    );
  });

  it("serves the list from memory for a minute, then logs in again only after the session ends", async () => {
    const calls: string[] = [];
    vi.stubGlobal("fetch", await fakeBitwarden(calls));
    await fetchSecretNames({ accessToken: TOKEN, region: "eu", now: 1 });
    await fetchSecretNames({ accessToken: TOKEN, region: "eu", now: 30_000 });
    expect(calls).toHaveLength(2);
    await fetchSecretNames({ accessToken: TOKEN, region: "eu", now: 61_000 });
    expect(calls.filter((u) => u.endsWith("/connect/token"))).toHaveLength(1);
    expect(calls).toHaveLength(3);
  });

  it("is unavailable when Bitwarden refuses, without leaking the token", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("no", { status: 401 })),
    );
    expect(
      await fetchSecretNames({ accessToken: TOKEN, region: "eu" }),
    ).toEqual({ state: "unavailable", secrets: [] });
    expect(JSON.stringify(warn.mock.calls)).not.toContain("client-secret");
  });
});
