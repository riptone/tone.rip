import { describe, expect, it } from "vitest";
import {
  ALL_APP_TAGS,
  resolveProbePath,
  SELF_HOSTED_APPS,
} from "../src/self-hosted-apps";

describe("SELF_HOSTED_APPS", () => {
  it("every app has a unique name and an https href", () => {
    const names = new Set(SELF_HOSTED_APPS.map((app) => app.name));
    expect(names.size).toBe(SELF_HOSTED_APPS.length);
    for (const app of SELF_HOSTED_APPS) {
      expect(app.href.startsWith("https://")).toBe(true);
      expect(app.tags.length).toBeGreaterThan(0);
    }
  });
});

describe("ALL_APP_TAGS", () => {
  it("is the deduped, sorted union of every app's tags", () => {
    expect(ALL_APP_TAGS).toEqual([...ALL_APP_TAGS].sort());
    expect(new Set(ALL_APP_TAGS).size).toBe(ALL_APP_TAGS.length);
  });
});

describe("resolveProbePath", () => {
  it("probes Vaultwarden's favicon instead of / (it 200s behind an auth wall)", () => {
    expect(resolveProbePath(new URL("https://pass.tone.rip"))).toBe(
      "/favicon.ico",
    );
  });

  it("probes / for every other app", () => {
    expect(resolveProbePath(new URL("https://photos.tone.rip"))).toBe("/");
  });
});
