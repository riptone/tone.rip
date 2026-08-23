import { expect, test } from "@playwright/test";
import { collectConsoleProblems } from "@repo/playwright-config";

/* This runs against `wrangler dev` (see playwright.config.ts), which builds
   the real, strict, nonce'd CSP the way production does - `astro dev` does
   not (docs/engineering.md). That is what makes the "no console/CSP
   problems" assertions below meaningful rather than a check against a
   relaxed policy that would pass regardless of a real regression. */

test("home page loads clean and the language switch works", async ({
  page,
}) => {
  const problems = collectConsoleProblems(page);

  await page.goto("/");
  const heading = page.locator("h1");
  await expect(heading).toHaveText("Hi, I'm tone.");

  await page.locator('[data-lang-set="pt"]').click();
  await expect(heading).toHaveText("Olá, sou o tone.");

  expect(problems.errors, problems.errors.join("\n")).toEqual([]);
  expect(problems.cspViolations, problems.cspViolations.join("\n")).toEqual([]);
});

/* The exact bug class docs/engineering.md documents under "Per-request
   nonces and soft navigation do not mix": a client-side router that parses
   the next response against the previous document's nonce produces CSP
   violations that are otherwise invisible until deployed. This app fixed it
   by moving to a real cross-document navigation
   (`packages/ui/src/styles/view-transition.css`) - a regression back to a
   fetch-and-parse router would reintroduce exactly this failure, and this is
   the one place that shows up. */
test("navigating to another page produces no CSP violation", async ({
  page,
}) => {
  const problems = collectConsoleProblems(page);

  await page.goto("/");
  await page.getByRole("link", { name: "work", exact: true }).click();
  await expect(page).toHaveURL(/\/work$/);
  await expect(page.locator("h1")).toHaveText("Work");

  expect(problems.errors, problems.errors.join("\n")).toEqual([]);
  expect(problems.cspViolations, problems.cspViolations.join("\n")).toEqual([]);
});

test("the work page's filter narrows the visible projects", async ({
  page,
}) => {
  const problems = collectConsoleProblems(page);

  // repos.astro renders this list from a real, live GitHub API call
  // (services/projects.ts). Skip rather than fail if GitHub has nothing
  // public to show: that's the page's own documented empty state, not a
  // filter regression.
  await page.goto("/work");
  // A single compound selector, not `items.locator(":visible")`: a chained
  // locator queries *inside* each matched element, so that would ask "does
  // this <li> contain a visible descendant" (almost always true - it has
  // several child tags) rather than "is this <li> itself visible", wildly
  // over-counting.
  const items = page.locator("[data-filter-item]");
  const visibleItems = page.locator("[data-filter-item]:visible");
  const totalCount = await items.count();
  test.skip(totalCount === 0, "no public repos returned to filter");

  await page.locator("[data-filter-search]").fill("zzz-no-such-project");
  await expect(page.locator("[data-filter-empty]")).toBeVisible();
  await expect(visibleItems).toHaveCount(0);

  await page.locator("[data-filter-search]").fill("");
  await expect(visibleItems).toHaveCount(totalCount);

  expect(problems.errors, problems.errors.join("\n")).toEqual([]);
  expect(problems.cspViolations, problems.cspViolations.join("\n")).toEqual([]);
});
