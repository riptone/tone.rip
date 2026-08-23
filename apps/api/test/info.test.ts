import { SELF } from "cloudflare:test";
import { describe, expect, it } from "vitest";

describe("GET /info/:slug", () => {
  it("renders the JSX HTML page for browsers", async () => {
    const res = await SELF.fetch("https://api.tone.rip/info/tone");
    expect(res.status).toBe(200);
    expect(res.headers.get("Content-Type")).toContain("text/html");
    expect(await res.text()).toContain("<h1>tone</h1>");
  });

  it("serves the markdown variant to agents", async () => {
    const res = await SELF.fetch("https://api.tone.rip/info/dashboard", {
      headers: { Accept: "text/markdown" },
    });
    expect(res.status).toBe(200);
    expect(res.headers.get("Content-Type")).toBe(
      "text/markdown; charset=utf-8",
    );
    expect(await res.text()).toContain("# main-menu");
  });

  it("404s for an unknown slug", async () => {
    const res = await SELF.fetch("https://api.tone.rip/info/nonexistent");
    expect(res.status).toBe(404);
  });
});
