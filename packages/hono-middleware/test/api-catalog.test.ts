import { Hono } from "hono";
import { describe, expect, it } from "vitest";
import { apiCatalog } from "../src/api-catalog";

describe("apiCatalog", () => {
  it("serves an RFC 9727 linkset+json document at the catalog path", async () => {
    const app = new Hono();
    app.use(
      apiCatalog({
        entries: [{ href: "https://tone.rip/api/projects.json" }],
      }),
    );
    app.get("/*", (c) => c.text("not the catalog"));

    const res = await app.request("https://tone.rip/.well-known/api-catalog");
    expect(res.headers.get("Content-Type")).toBe(
      "application/linkset+json; charset=utf-8",
    );
    const body = await res.json();
    expect(body.linkset[0].anchor).toBe("https://tone.rip/api/projects.json");
    expect(body.linkset[0]["service-desc"][0].type).toBe("application/json");
  });

  it("falls through to the next handler for any other path", async () => {
    const app = new Hono();
    app.use(apiCatalog({ entries: [] }));
    app.get("/*", (c) => c.text("passthrough"));

    const res = await app.request("https://tone.rip/");
    expect(await res.text()).toBe("passthrough");
  });
});
