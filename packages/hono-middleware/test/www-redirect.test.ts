import { Hono } from "hono";
import { describe, expect, it } from "vitest";
import { wwwRedirect } from "../src/www-redirect";

function buildApp() {
  const app = new Hono();
  app.use(wwwRedirect({ apexHost: "tone.rip" }));
  app.get("/*", (c) => c.text("ok"));
  return app;
}

describe("wwwRedirect", () => {
  it("301s www to the apex host, preserving path", async () => {
    const res = await buildApp().request("https://www.tone.rip/projects", {
      redirect: "manual",
    });
    expect(res.status).toBe(301);
    expect(res.headers.get("Location")).toBe("https://tone.rip/projects");
  });

  it("upgrades a plaintext request to https rather than mirroring the scheme", async () => {
    // Mirroring it costs a second plaintext hop, taken before HSTS is ever
    // set on the apex - which is exactly when it matters most.
    const res = await buildApp().request("http://www.tone.rip/projects?a=1", {
      redirect: "manual",
    });
    expect(res.status).toBe(301);
    expect(res.headers.get("Location")).toBe("https://tone.rip/projects?a=1");
  });

  it("passes through requests already on the apex host", async () => {
    const res = await buildApp().request("https://tone.rip/projects");
    expect(res.status).toBe(200);
    expect(await res.text()).toBe("ok");
  });
});
