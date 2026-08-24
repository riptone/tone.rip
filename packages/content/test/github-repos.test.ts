import { describe, expect, it } from "vitest";
import { latestUpdateTimestamp, simplifyRepos } from "../src/github-repos";

describe("simplifyRepos", () => {
  it("maps GitHub's repo shape to the simplified shape", () => {
    const result = simplifyRepos([
      {
        name: "tonil",
        html_url: "https://github.com/no-tone/tonil",
        stargazers_count: 3,
        updated_at: "2026-01-01T00:00:00Z",
      },
    ]);
    expect(result).toEqual([
      {
        name: "tonil",
        url: "https://github.com/no-tone/tonil",
        homepage: "",
        language: "Other",
        description: "",
        topics: [],
        isFork: false,
        isArchived: false,
        hasPages: false,
        stars: 3,
        forks: 0,
        updatedAt: "2026-01-01T00:00:00Z",
      },
    ]);
  });

  it("drops entries without a name or html_url", () => {
    expect(
      simplifyRepos([{ name: "no-url" }, { html_url: "no-name" }]),
    ).toEqual([]);
  });

  it("returns an empty array for non-array input", () => {
    expect(simplifyRepos(null)).toEqual([]);
    expect(simplifyRepos({})).toEqual([]);
  });
});

describe("latestUpdateTimestamp", () => {
  it("returns the most recent updatedAt across repos", () => {
    const repos = simplifyRepos([
      {
        name: "a",
        html_url: "https://x/a",
        updated_at: "2025-01-01T00:00:00Z",
      },
      {
        name: "b",
        html_url: "https://x/b",
        updated_at: "2026-01-01T00:00:00Z",
      },
    ]);
    expect(latestUpdateTimestamp(repos)).toBe("2026-01-01T00:00:00.000Z");
  });

  it("returns an empty string when no repo has a timestamp", () => {
    expect(latestUpdateTimestamp([])).toBe("");
  });
});
