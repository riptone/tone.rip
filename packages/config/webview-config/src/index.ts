/// <reference types="bun-types" />
/* Shared helper for Bun.WebView e2e — mirrors @repo/playwright-config
   for the pilot that runs alongside Playwright. Keep until cutover. */

const CSP_VIOLATION_PATTERN = /content security policy|refused to/i;

export interface WebViewConsoleProblems {
  errors: string[];
  cspViolations: string[];
}

/**
 * Returns { problems, handler }.
 *
 * Pass handler as `new Bun.WebView({ console: handler })`.
 * The handler classifies browser `console.error` text the same way
 * Playwright's collectConsoleProblems does: CSP `refused to…` vs other errors.
 *
 * Page-side `console.error` already includes CSP violations; uncaught
 * exceptions also surface as `error` there, so one collector covers both
 * Playwright's `page.on("console")` + `page.on("pageerror")`.
 */
export function collectWebViewProblems(): {
  problems: WebViewConsoleProblems;
  handler: (type: string, ...args: unknown[]) => void;
} {
  const problems: WebViewConsoleProblems = { errors: [], cspViolations: [] };
  const handler = (type: string, ...args: unknown[]): void => {
    if (type !== "error") return;
    const text = args
      .map((a) => {
        if (typeof a === "string") return a;
        if (a && typeof a === "object") {
          const o = a as Record<string, unknown>;
          // Chrome CDP RemoteObject has `description`; WebKit falls back to String()
          if (typeof o.description === "string") return o.description as string;
          if (typeof o.value === "string") return o.value as string;
          try {
            return JSON.stringify(a);
          } catch {
            return String(a);
          }
        }
        return String(a);
      })
      .join(" ");
    if (CSP_VIOLATION_PATTERN.test(text)) problems.cspViolations.push(text);
    else problems.errors.push(text);
  };
  return { problems, handler };
}

/** Backend for Bun.WebView: webkit on macOS (zero-install), chrome elsewhere (CI). */
export function getWebViewBackend(): Bun.WebView.ConstructorOptions["backend"] {
  if (process.env.BUN_WEBVIEW_BACKEND) {
    return process.env
      .BUN_WEBVIEW_BACKEND as Bun.WebView.ConstructorOptions["backend"];
  }
  return process.platform === "darwin" ? undefined : "chrome";
}

/** Create a WebView with the right backend for this platform. */
export function createWebView(
  options: Omit<Bun.WebView.ConstructorOptions, "backend"> & {
    backend?: Bun.WebView.ConstructorOptions["backend"];
  } = {},
): Bun.WebView {
  const backend = options.backend ?? getWebViewBackend();
  return new Bun.WebView(
    backend === undefined ? options : { ...options, backend },
  );
}

/** Poll fetch(baseURL) until 200 or timeout. Mirrors Playwright webServer wait. */
export async function ensureServer(
  baseURL: string,
  timeoutMs = 120_000,
): Promise<void> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    try {
      const res = await fetch(baseURL, { method: "HEAD" });
      if (res.ok || res.status < 500) return;
    } catch {}
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error(`Server not ready at ${baseURL} after ${timeoutMs}ms`);
}

/** Evaluate helper — unwrap nullish to string. */
export async function getText(
  view: Bun.WebView,
  selector: string,
): Promise<string | null> {
  return (await view.evaluate(
    `document.querySelector(${JSON.stringify(selector)})?.textContent ?? null`,
  )) as string | null;
}

/** Wait for element text to equal expected, polling via evaluate. */
export async function waitForText(
  view: Bun.WebView,
  selector: string,
  expected: string,
  timeoutMs = 5000,
): Promise<void> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    const text = await getText(view, selector);
    if (text?.trim() === expected) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  const actual = await getText(view, selector);
  throw new Error(
    `waitForText timeout: expected ${JSON.stringify(expected)} at ${selector}, got ${JSON.stringify(actual)}`,
  );
}

/**
 * Scroll an element into view, then click it.
 *
 * Playwright's `.click()` scrolls first. Bun.WebView's does not: its
 * actionability check requires the element to already *be* in the viewport,
 * and an element that never gets there is waited out for the full 30s
 * default. A footer button below the fold therefore fails as a bare
 * "timed out after 30000ms" with no selector and no clue - which is exactly
 * how the language switch failed in CI.
 *
 * `block: "nearest"` mirrors Playwright's minimal scroll: a no-op when the
 * element is already visible, so this stays a safe default for every click.
 *
 * The shorter timeout is the other half of it. A real regression should say
 * which selector never became clickable, within seconds, rather than eating
 * the whole test budget on the way to saying nothing.
 */
export async function clickInView(
  view: Bun.WebView,
  selector: string,
  timeoutMs = 5000,
): Promise<void> {
  await view.scrollTo(selector, { block: "nearest", timeout: timeoutMs });
  await view.click(selector, { timeout: timeoutMs });
}

/** Count visible `[data-filter-item]` (element not hidden, has bounding box). */
export async function countVisibleItems(view: Bun.WebView): Promise<number> {
  return (await view.evaluate(
    `[...document.querySelectorAll("[data-filter-item]")].filter(el => !el.hidden && el.getBoundingClientRect().height > 0).length`,
  )) as number;
}

/** Fill search box: click to focus, select-all, type. Fires beforeinput/input with isTrusted:true. */
export async function fillSearch(
  view: Bun.WebView,
  text: string,
): Promise<void> {
  await clickInView(view, "[data-filter-search]");
  // Clear existing via evaluate (select-all + delete is flaky across backends)
  await view.evaluate(
    `(() => { const el = document.querySelector("[data-filter-search]"); if (el) { el.focus(); el.value = ""; el.dispatchEvent(new Event("input", { bubbles: true })); } })()`,
  );
  if (text) await view.type(text);
}
