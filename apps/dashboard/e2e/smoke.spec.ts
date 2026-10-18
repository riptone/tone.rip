import { expect, test } from "@playwright/test";
import { collectConsoleProblems } from "@repo/playwright-config";

/* This runs against `wrangler dev` (see playwright.config.ts), which builds
   the real, strict, nonce'd CSP the way production does - `astro dev` does
   not (docs/engineering.md). That is what makes the "no console/CSP
   problems" assertions below meaningful. */

/*
 * The board used to probe every tile's real host from the browser to show it
 * as up/down, so this suite answered every cross-origin request with a
 * harmless 200 rather than ping someone's actual password manager on every
 * run. The probes are gone, and so is the reason to tolerate any
 * cross-origin traffic: connect-src is 'self' now, and the fixtures carry no
 * icons. So the stub stays - nothing reaches real infrastructure even if a
 * regression starts making requests - but it also records them, and the
 * tests assert there were none.
 */
async function recordExternalRequests(
  page: import("@playwright/test").Page,
  baseURL: string,
): Promise<string[]> {
  const sameOrigin = new URL(baseURL).origin;
  const external: string[] = [];
  await page.route(
    (url) => url.origin !== sameOrigin,
    (route) => {
      external.push(route.request().url());
      return route.fulfill({ status: 200, body: "" });
    },
  );
  return external;
}

test("the launcher loads clean and lists the self-hosted apps", async ({
  page,
  baseURL,
}) => {
  const problems = collectConsoleProblems(page);
  const external = await recordExternalRequests(page, baseURL ?? "");

  await page.goto("/");
  await expect(page.locator("h1")).toHaveText("self-hosted");
  await expect(page.locator("[data-filter-item]").first()).toBeVisible();

  expect(problems.errors, problems.errors.join("\n")).toEqual([]);
  expect(problems.cspViolations, problems.cspViolations.join("\n")).toEqual([]);
  expect(external, external.join("\n")).toEqual([]);
});

test("the filter narrows the visible tiles", async ({ page, baseURL }) => {
  const problems = collectConsoleProblems(page);
  const external = await recordExternalRequests(page, baseURL ?? "");

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
  expect(external, external.join("\n")).toEqual([]);
});

test("the secrets section lists names only, collapsed until opened", async ({
  page,
  baseURL,
}) => {
  const problems = collectConsoleProblems(page);
  const external = await recordExternalRequests(page, baseURL ?? "");

  await page.goto("/");
  const section = page.locator("[data-secrets]");
  await expect(section).not.toHaveAttribute("open");
  await section.locator("summary").click();
  await expect(section.locator(".secret").first()).toBeVisible();
  await expect(section.locator(".secret__name").first()).toHaveText(
    "EXAMPLE_API_KEY",
  );
  // Values are read in Bitwarden, not here.
  await expect(section.locator("[data-vault]")).toHaveAttribute(
    "href",
    /^https:\/\/vault\.bitwarden\.eu\/#\/sm\/[0-9a-f-]+\/secrets$/,
  );

  expect(problems.errors, problems.errors.join("\n")).toEqual([]);
  expect(problems.cspViolations, problems.cspViolations.join("\n")).toEqual([]);
  expect(external, external.join("\n")).toEqual([]);
});

test("the search box narrows the secrets too, and opens them on a match", async ({
  page,
}) => {
  await page.goto("/");
  const section = page.locator("[data-secrets]");
  await page.locator("[data-filter-search]").fill("webhook");
  await expect(section).toHaveAttribute("open");
  await expect(section.locator(".secret:visible")).toHaveCount(1);
  await expect(section.locator(".secret__name:visible")).toHaveText(
    "EXAMPLE_WEBHOOK_SECRET",
  );
  await expect(section.locator("[data-secrets-count]")).toHaveText(
    /^1 \/ \d+$/,
  );
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
