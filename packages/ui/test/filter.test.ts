import { describe, expect, it } from "vitest";
import { matchesFilter } from "../src/site/filter.js";

describe("matchesFilter", () => {
  const tile = { name: "Containers", tags: "Ops,Self-Hosted" };

  it("matches everything when query and tag are empty", () => {
    expect(matchesFilter(tile, "", "")).toBe(true);
  });

  it("matches by case-insensitive substring of the name", () => {
    expect(matchesFilter(tile, "cont", "")).toBe(true);
    expect(matchesFilter(tile, "CONT", "")).toBe(true);
    expect(matchesFilter(tile, "nothing-like-it", "")).toBe(false);
  });

  it("matches by tag membership in the comma-joined tag string", () => {
    expect(matchesFilter(tile, "", "Ops")).toBe(true);
    expect(matchesFilter(tile, "", "Media")).toBe(false);
  });

  it("requires both the name and tag filters to match", () => {
    expect(matchesFilter(tile, "cont", "Ops")).toBe(true);
    expect(matchesFilter(tile, "cont", "Media")).toBe(false);
  });

  it("trims whitespace from both filters", () => {
    expect(matchesFilter(tile, "  cont  ", "  Ops  ")).toBe(true);
  });
});
