import type { SelfHostedApp } from "@repo/validation";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  findTailnetDevice,
  probeAllApps,
  probeAppHealth,
} from "../src/services/app-health";

describe("probeAppHealth", () => {
  const originalFetch = globalThis.fetch;

  afterEach(() => {
    vi.unstubAllGlobals();
    globalThis.fetch = originalFetch;
  });

  it("returns 'unknown' for an unparseable href instead of throwing", async () => {
    expect(await probeAppHealth("not a url")).toBe("unknown");
  });

  it("returns 'up' when the probe responds under 500", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(null, { status: 200 })),
    );
    expect(await probeAppHealth("https://example.com")).toBe("up");
  });

  it("returns 'down' when the probe responds 5xx or throws", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(null, { status: 503 })),
    );
    expect(await probeAppHealth("https://example.com")).toBe("down");

    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("network down");
      }),
    );
    expect(await probeAppHealth("https://example.com")).toBe("down");
  });

  it("probes Vaultwarden's favicon instead of / (it 200s behind an auth wall)", async () => {
    const fetchMock = vi.fn(
      async (_input: string | URL | Request) =>
        new Response(null, { status: 200 }),
    );
    vi.stubGlobal("fetch", fetchMock);
    await probeAppHealth("https://pass.tone.rip");
    const requestedUrl = new URL(String(fetchMock.mock.calls[0]?.[0]));
    expect(requestedUrl.pathname).toBe("/favicon.ico");
  });
});

describe("probeAllApps", () => {
  const originalFetch = globalThis.fetch;

  afterEach(() => {
    vi.unstubAllGlobals();
    globalThis.fetch = originalFetch;
  });

  const app = (name: string, tags: SelfHostedApp["tags"]): SelfHostedApp => ({
    name,
    href: `https://${name}.example.com`,
    tags,
    iconUrl: "",
  });

  it("reports 'unknown' for tailnet-only apps without probing them at all", async () => {
    // Cloudflare's edge answers these with a 403 regardless of whether the
    // box is alive, so a probe can only ever produce a false "up".
    const fetchMock = vi.fn(async () => new Response(null, { status: 403 }));
    vi.stubGlobal("fetch", fetchMock);

    const [result] = await probeAllApps([app("pass", ["Self-Hosted"])]);
    expect(result?.status).toBe("unknown");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("still probes genuinely public apps", async () => {
    const fetchMock = vi.fn(async () => new Response(null, { status: 302 }));
    vi.stubGlobal("fetch", fetchMock);

    const [result] = await probeAllApps([app("tailscale", ["Network"])]);
    expect(result?.status).toBe("up");
    expect(fetchMock).toHaveBeenCalledOnce();
  });
});

describe("findTailnetDevice", () => {
  const devices = [
    {
      hostname: "laptop.tailnet.ts.net",
      name: "laptop",
      addresses: ["100.1.2.3"],
    },
    {
      hostname: "server.tailnet.ts.net",
      name: "server",
      addresses: ["100.4.5.6"],
    },
  ];

  it("matches by exact hostname, name, or address", () => {
    expect(findTailnetDevice(devices, "server")?.name).toBe("server");
    expect(findTailnetDevice(devices, "100.1.2.3")?.name).toBe("laptop");
  });

  it("matches a bare hostname against the dotted tailnet FQDN", () => {
    expect(findTailnetDevice(devices, "laptop.tailnet.ts.net")?.name).toBe(
      "laptop",
    );
  });

  it("returns null for no match or an empty target", () => {
    expect(findTailnetDevice(devices, "unknown-device")).toBeNull();
    expect(findTailnetDevice(devices, "")).toBeNull();
  });
});
