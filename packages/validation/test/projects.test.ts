import { describe, expect, it } from "vitest";
import { projectsResponseSchema } from "../src/projects";

/** One entry shaped the way apps/api actually serves it. */
const REAL = {
  name: "tone.rip",
  url: "https://github.com/riptone/tone.rip",
  homepage: "https://tone.rip",
  language: "TypeScript",
  description: "Monorepo for tone.rip",
  topics: ["astro", "bun"],
  isFork: false,
  isArchived: false,
  hasPages: false,
  forks: 0,
  stars: 3,
  updatedAt: "2026-08-01T10:00:00Z",
};

describe("projectsResponseSchema", () => {
  it("accepts what the API serves, extra fields and all", () => {
    const parsed = projectsResponseSchema.safeParse([REAL]);
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[0]?.name).toBe("tone.rip");
  });

  it("rejects raw GitHub payloads rather than silently returning nothing", () => {
    // The bug this schema replaced: /projects is already simplified, and
    // running simplifyRepos over it a second time looked for `html_url` and
    // `fork`, found neither, and dropped every row - an empty page with no
    // error anywhere. A raw payload must fail loudly instead.
    const raw = [
      { name: "tone.rip", html_url: "https://github.com/riptone/tone.rip" },
    ];
    const parsed = projectsResponseSchema.safeParse(raw);
    expect(parsed.success && parsed.data).toEqual([]);
  });

  it("drops a single malformed entry and keeps the rest", () => {
    const parsed = projectsResponseSchema.safeParse([
      REAL,
      { name: "broken", url: "not-a-url" },
      { ...REAL, name: "other" },
    ]);
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data.map((p) => p.name)).toEqual([
      "tone.rip",
      "other",
    ]);
  });

  it("fills in the optional fields so the page never reads undefined", () => {
    const parsed = projectsResponseSchema.safeParse([
      { name: "bare", url: "https://github.com/riptone/bare" },
    ]);
    expect(parsed.success && parsed.data[0]).toMatchObject({
      description: "",
      language: "Other",
      topics: [],
      stars: 0,
      isFork: false,
    });
  });

  it("fails when the body is not a list at all", () => {
    expect(projectsResponseSchema.safeParse({ error: "nope" }).success).toBe(
      false,
    );
  });
});
