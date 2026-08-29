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

/* The 404 answers 404.

   `src/pages/404.astro` now re-exports a shared component
   (`@repo/ui/site/NotFound.astro`), and both apps' copies were previously
   free to drift - one of them had already been a `<meta http-equiv="refresh">`
   bounce to `/`, which answered every mistyped URL with a redirect and a 200.
   A not-found page that does not say "not found" is invisible to everything
   except a crawler and a confused reader, so the status code is asserted
   rather than the markup. */
test("an unknown URL answers 404, with the field mounted", async ({ page }) => {
  const problems = collectConsoleProblems(page);

  const response = await page.goto("/this-page-does-not-exist");
  expect(response?.status()).toBe(404);

  // The ramp is declared on <body> and read by the shared component's script;
  // if that contract breaks the field silently falls back to the default.
  await expect(page.locator("body")).toHaveAttribute("data-ramp", "dusk");
  await expect(page.locator(".notfound__digit")).toHaveCount(3);
  await expect(page.locator("[data-gradient-panel] canvas")).toBeAttached();

  /* Only the CSP assertion here, not `problems.errors`. Chromium logs
     "Failed to load resource: the server responded with a status of 404" for
     the navigation itself, so a 404 page can never have an empty error list -
     asserting it would mean either deleting the status code this test exists
     to check, or filtering by message text and calling it a pass. The CSP
     check is the one that can actually catch something: this page runs its
     own inline field script under the same strict nonce'd policy. */
  expect(problems.cspViolations, problems.cspViolations.join("\n")).toEqual([]);
});
