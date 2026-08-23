import { Hono } from "hono";
import { describe, expect, it } from "vitest";
import { markdownNegotiation } from "../src/markdown-negotiation";

function buildApp() {
  const app = new Hono();
  app.use(markdownNegotiation({ markdown: "# tone" }));
  app.get("/", (c) => c.html("<html>app shell</html>"));
  return app;
}

describe("markdownNegotiation", () => {
  it("returns markdown when the agent asks for it", async () => {
    const res = await buildApp().request("/", {
      headers: { Accept: "text/markdown" },
    });
    expect(res.headers.get("Content-Type")).toBe(
      "text/markdown; charset=utf-8",
    );
    expect(res.headers.get("Vary")).toBe("Accept");
    expect(await res.text()).toBe("# tone");
  });

  it("falls through to the normal app for browsers", async () => {
    const res = await buildApp().request("/", {
      headers: { Accept: "text/html" },
    });
    expect(await res.text()).toBe("<html>app shell</html>");
  });
});
