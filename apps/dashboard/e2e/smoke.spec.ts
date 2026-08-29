import { expect, test } from "@playwright/test";
import { collectConsoleProblems } from "@repo/playwright-config";

/* This runs against `wrangler dev` (see playwright.config.ts), which builds
   the real, strict, nonce'd CSP the way production does - `astro dev` does
   not (docs/engineering.md). That is what makes the "no console/CSP
   problems" assertions below meaningful. */

/*
 * dashboard.ts pings every tile's real host on load (client-probe.ts) to
 * show it as up/down - by design, since that's the whole point of the
 * launcher. A CI run has no route to any of them (they're behind the user's
 * own Tailscale/home network), and even where one resolved, having this
 * suite ping someone's actual password manager on every run is not
 * something a test should do as a side effect. Answer every cross-origin
 * request with a harmless 200 so the probe logic still runs (and doesn't
 * itself log anything) without depending on, or touching, real
 * infrastructure.
 */
async function stubExternalProbes(
  page: import("@playwright/test").Page,
  baseURL: string,
): Promise<void> {
  const sameOrigin = new URL(baseURL).origin;
  await page.route(
    (url) => url.origin !== sameOrigin,
    (route) => route.fulfill({ status: 200, body: "" }),
  );
}

test("the launcher loads clean and lists the self-hosted apps", async ({
  page,
  baseURL,
}) => {
  const problems = collectConsoleProblems(page);
  await stubExternalProbes(page, baseURL ?? "");

  await page.goto("/");
  await expect(page.locator("h1")).toHaveText("self-hosted");
  await expect(page.locator("[data-filter-item]").first()).toBeVisible();

  expect(problems.errors, problems.errors.join("\n")).toEqual([]);
  expect(problems.cspViolations, problems.cspViolations.join("\n")).toEqual([]);
});

test("the filter narrows the visible tiles", async ({ page, baseURL }) => {
  const problems = collectConsoleProblems(page);
  await stubExternalProbes(page, baseURL ?? "");

  await page.goto("/");
  // A single compound selector, not `items.locator(":visible")`: a chained
  // locator queries *inside* each matched element, so that asks "does this
  // tile contain a visible descendant" (almost always true) rather than "is
  // this tile itself visible".
  const items = page.locator("[data-filter-item]");
  const visibleItems = page.locator("[data-filter-item]:visible");
  const totalCount = await items.count();
  expect(totalCount).toBeGreaterThan(1);

  await page.locator("[data-filter-search]").fill("Gallery");
  await expect(visibleItems).toHaveCount(1);

  await page.locator("[data-filter-search]").fill("zzz-no-such-app");
  await expect(page.locator("[data-filter-empty]")).toBeVisible();
  await expect(visibleItems).toHaveCount(0);

  await page.locator("[data-filter-search]").fill("");
  await expect(visibleItems).toHaveCount(totalCount);

  expect(problems.errors, problems.errors.join("\n")).toEqual([]);
  expect(problems.cspViolations, problems.cspViolations.join("\n")).toEqual([]);
});

/* This app's 404 used to be a `<meta http-equiv="refresh">` bounce to `/`,
   which behind Cloudflare Access made a typo and an authorisation problem
   look identical. It is now the same shared component apps/web renders, so
   the thing worth asserting here is that this app's own props reach it. */
test("an unknown URL answers 404 with this app's palette", async ({ page }) => {
  const response = await page.goto("/this-page-does-not-exist");
  expect(response?.status()).toBe(404);
  await expect(page.locator("body")).toHaveAttribute("data-ramp", "glacier");
  await expect(page.locator(".notfound__digit")).toHaveCount(3);
});
