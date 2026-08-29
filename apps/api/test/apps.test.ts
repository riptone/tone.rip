import { SELF } from "cloudflare:test";
import { toSelfHostedApps } from "@repo/validation";
import { afterEach, describe, expect, it, vi } from "vitest";

/* The mapping is pure, so it is tested directly rather than through a fetch.
   Every case here is a shape Cloudflare genuinely returns. */
describe("toSelfHostedApps", () => {
  it("maps a self-hosted application onto a tile", () => {
    expect(
      toSelfHostedApps([
        {
          id: "1",
          name: "Vaultwarden",
          domain: "pass.tone.rip",
          type: "self_hosted",
          logo_url: "https://cdn.example/vaultwarden.webp",
          tags: ["Security", "Self-Hosted"],
        },
      ]),
    ).toEqual([
      {
        name: "Vaultwarden",
        href: "https://pass.tone.rip/",
        tags: ["Security", "Self-Hosted"],
        iconUrl: "https://cdn.example/vaultwarden.webp",
      },
    ]);
  });

  // An application can exist with no domain (a draft, an infrastructure
  // target). A tile linking to "https://undefined" is worse than no tile.
  it("drops applications with no domain or no name", () => {
    expect(
      toSelfHostedApps([
        { id: "1", name: "No domain", type: "self_hosted" },
        { id: "2", domain: "nameless.tone.rip", type: "self_hosted" },
      ]),
    ).toEqual([]);
  });

  // Access covers things that are not web pages at all. They belong to the
  // account, not to a launcher.
  it("drops application types a launcher cannot open", () => {
    const apps = toSelfHostedApps([
      { id: "1", name: "Box", domain: "ssh.tone.rip", type: "ssh" },
      {
        id: "2",
        name: "Notes",
        domain: "notes.tone.rip",
        type: "self_hosted",
        tags: ["Self-Hosted"],
      },
    ]);
    expect(apps.map((a) => a.name)).toEqual(["Notes"]);
  });

  // logo_url is optional in the API and unset on a fresh application, so its
  // absence has to be a tile without an icon rather than a validation failure
  // that costs the whole list.
  it("survives an application with no logo", () => {
    expect(
      toSelfHostedApps([
        {
          id: "1",
          name: "Plain",
          domain: "plain.tone.rip",
          type: "self_hosted",
          tags: ["Self-Hosted"],
        },
      ]),
    ).toEqual([
      {
        name: "Plain",
        href: "https://plain.tone.rip/",
        tags: ["Self-Hosted"],
        iconUrl: null,
      },
    ]);
  });

  // The icon becomes an <img src> on an https page, where an http URL is
  // blocked anyway - so admitting it would only produce a broken tile.
  it("refuses a non-https logo rather than rendering a blocked image", () => {
    const [app] = toSelfHostedApps([
      {
        id: "1",
        name: "Insecure",
        domain: "x.tone.rip",
        type: "self_hosted",
        tags: ["Self-Hosted"],
        logo_url: "http://cdn.example/x.png",
      },
    ]);
    expect(app?.iconUrl).toBeNull();
  });

  // The domain is typed into a form by an administrator and ends up in the
  // dashboard's connect-src. Anything new URL() would throw on is dropped
  // here, where it costs one tile instead of a 500.
  it("drops a domain that cannot be parsed", () => {
    expect(
      toSelfHostedApps([
        { id: "1", name: "Broken", domain: "not a host", type: "self_hosted" },
      ]),
    ).toEqual([]);
  });

  it("accepts a domain that already carries a scheme", () => {
    const [app] = toSelfHostedApps([
      {
        id: "1",
        name: "Scheme",
        domain: "https://scheme.tone.rip",
        type: "self_hosted",
        tags: ["Self-Hosted"],
      },
    ]);
    expect(app?.href).toBe("https://scheme.tone.rip/");
  });

  // Inclusion is opt-in: an Access account holds more than a launcher's worth
  // of applications, so an untagged one is somebody else's business.
  it("drops applications without the launcher tag", () => {
    expect(
      toSelfHostedApps([
        {
          id: "1",
          name: "Internal",
          domain: "internal.tone.rip",
          type: "self_hosted",
          tags: ["Ops"],
        },
        {
          id: "2",
          name: "Untagged",
          domain: "untagged.tone.rip",
          type: "self_hosted",
        },
      ]),
    ).toEqual([]);
  });

  // The tag is typed by hand into a form. Comparing a lowercased tag straight
  // against a capitalised constant matches nothing and empties the launcher
  // silently, which is a bug that looks exactly like "the API is broken".
  it("matches the launcher tag whatever case it was typed in", () => {
    const apps = toSelfHostedApps([
      {
        id: "1",
        name: "Shouty",
        domain: "a.tone.rip",
        type: "self_hosted",
        tags: ["SELF-HOSTED"],
      },
      {
        id: "2",
        name: "Padded",
        domain: "b.tone.rip",
        type: "self_hosted",
        tags: ["  self-hosted  "],
      },
    ]);
    expect(apps.map((a) => a.name)).toEqual(["Padded", "Shouty"]);
  });

  // The launcher tag is kept in the data even though the UI hides it:
  // apps/api's isTailnetOnly reads it to decide whether the edge may probe a
  // host, and a host on Tailscale CGNAT is unreachable from Cloudflare. Strip
  // it here and every tile gets probed and reported "down".
  it("keeps the launcher tag, because the probe depends on it", () => {
    const [app] = toSelfHostedApps([
      {
        id: "1",
        name: "Immich",
        domain: "photos.tone.rip",
        type: "self_hosted",
        tags: ["Self-Hosted", "Media"],
      },
    ]);
    expect(app?.tags).toContain("Self-Hosted");
    expect(app?.tags).toContain("Media");
  });

  // A stable order means an unchanged account produces an unchanged response,
  // so the edge cache is not invalidated by Cloudflare's own result ordering.
  it("sorts by name so the response is stable", () => {
    const apps = toSelfHostedApps([
      {
        id: "1",
        name: "Zulip",
        domain: "z.tone.rip",
        type: "self_hosted",
        tags: ["Self-Hosted"],
      },
      {
        id: "2",
        name: "Actual",
        domain: "a.tone.rip",
        type: "self_hosted",
        tags: ["Self-Hosted"],
      },
    ]);
    expect(apps.map((a) => a.name)).toEqual(["Actual", "Zulip"]);
  });
});

describe("GET /apps", () => {
  afterEach(() => vi.unstubAllGlobals());

  // No token is the expected state of a fork and of `wrangler dev`, so it has
  // to be a 200 with an empty list and a named state - not an error.
  it("answers 200 and says so when no token is configured", async () => {
    const res = await SELF.fetch("https://api.tone.rip/apps");
    expect(res.status).toBe(200);
    const body = (await res.json()) as { apps: unknown[]; state: string };
    expect(Array.isArray(body.apps)).toBe(true);
    expect([
      "unconfigured",
      "unavailable",
      "stale",
      "hit",
      "updated",
    ]).toContain(body.state);
  });

  it("is listed in the API catalog so it is discoverable", async () => {
    const res = await SELF.fetch(
      "https://api.tone.rip/.well-known/api-catalog",
    );
    expect(await res.text()).toContain("/apps");
  });
});
