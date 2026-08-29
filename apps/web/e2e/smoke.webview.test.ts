/// <reference types="bun-types" />
import { beforeAll, expect, test } from "bun:test";
import {
  collectWebViewProblems,
  countVisibleItems,
  createWebView,
  ensureServer,
  fillSearch,
  getText,
  waitForText,
} from "@repo/webview-config";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://localhost:8787";

beforeAll(async () => {
  await ensureServer(BASE_URL);
});

/* Runs against `wrangler dev` (astro build + wrangler dev --port 8787)
   which builds the real strict nonce'd CSP — same reason Playwright targets
   it. See packages/config/playwright-config and apps/web/playwright.config.ts. */

test("home page loads clean and the language switch works", async () => {
  const { problems, handler } = collectWebViewProblems();
  const view = createWebView({
    width: 1280,
    height: 720,
    console: handler,
  });
  try {
    await view.navigate(`${BASE_URL}/`);
    await waitForText(view, "h1", "Hi, I'm tone.");

    await view.click('[data-lang-set="pt"]');
    await waitForText(view, "h1", "Olá, sou o tone.");

    expect(problems.errors, problems.errors.join("\n")).toEqual([]);
    expect(problems.cspViolations, problems.cspViolations.join("\n")).toEqual(
      [],
    );
  } finally {
    view.close();
  }
});

test("navigating to another page produces no CSP violation", async () => {
  const { problems, handler } = collectWebViewProblems();
  const view = createWebView({
    width: 1280,
    height: 720,
    console: handler,
  });
  try {
    await view.navigate(`${BASE_URL}/`);
    // waitForText ensures heading ready before click — click(selector) also waits actionable
    await waitForText(view, "h1", "Hi, I'm tone.");
    await view.click('a[data-section="work"]');

    // navigate via click triggers onNavigated before next evaluate settles
    // poll URL and heading instead of relying on WebView.url timing
    const start = Date.now();
    while (Date.now() - start < 5000) {
      const url = view.url;
      if (url.endsWith("/work")) break;
      await new Promise((r) => setTimeout(r, 100));
    }
    expect(view.url).toMatch(/\/work$/);
    await waitForText(view, "h1", "Work");

    expect(problems.errors, problems.errors.join("\n")).toEqual([]);
    expect(problems.cspViolations, problems.cspViolations.join("\n")).toEqual(
      [],
    );
  } finally {
    view.close();
  }
});

test("the work page's filter narrows the visible projects", async () => {
  const { problems, handler } = collectWebViewProblems();
  const view = createWebView({
    width: 1280,
    height: 720,
    console: handler,
  });
  try {
    await view.navigate(`${BASE_URL}/work`);

    const totalCount = (await view.evaluate(
      `document.querySelectorAll("[data-filter-item]").length`,
    )) as number;
    if (totalCount === 0) {
      console.log("skip: no public repos returned to filter");
      return;
    }

    await fillSearch(view, "zzz-no-such-project");
    // empty notice becomes visible, visible items 0
    await view.evaluate(
      `new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))`,
    );
    const emptyVisible = (await view.evaluate(
      `!document.querySelector("[data-filter-empty]")!.hidden`,
    )) as boolean;
    expect(emptyVisible).toBe(true);
    expect(await countVisibleItems(view)).toBe(0);

    await fillSearch(view, "");
    expect(await countVisibleItems(view)).toBe(totalCount);

    // Optional sanity: count text matches total
    const countText = await getText(view, "[data-filter-count]");
    expect(countText?.trim()).toBe(`${totalCount} / ${totalCount}`);

    expect(problems.errors, problems.errors.join("\n")).toEqual([]);
    expect(problems.cspViolations, problems.cspViolations.join("\n")).toEqual(
      [],
    );
  } finally {
    view.close();
  }
});
