import { Hono } from "hono";
import { describe, expect, it } from "vitest";
import { devRobots } from "../src/dev-robots";

function buildApp() {
  const app = new Hono();
  app.use(devRobots({ devHostnames: ["dev.tone.rip"] }));
  app.get("/*", (c) => c.text("page"));
  return app;
}

describe("devRobots", () => {
  it("serves a blanket-disallow robots.txt on dev hosts", async () => {
    const res = await buildApp().request("https://dev.tone.rip/robots.txt");
    expect(await res.text()).toBe("User-agent: *\nDisallow: /\n");
    expect(res.headers.get("X-Robots-Tag")).toContain("noindex");
  });

  it("tags every other response on dev hosts with X-Robots-Tag", async () => {
    const res = await buildApp().request("https://dev.tone.rip/anything");
    expect(res.headers.get("X-Robots-Tag")).toContain("noindex");
  });

  it("does not interfere on production hosts", async () => {
    const res = await buildApp().request("https://tone.rip/robots.txt");
    expect(await res.text()).toBe("page");
    expect(res.headers.get("X-Robots-Tag")).toBeNull();
  });
});
