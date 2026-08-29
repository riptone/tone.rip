/* Shared between apps/web's and apps/dashboard's playwright.config.ts -
   both are Astro-on-Cloudflare Workers with the same shape of e2e problem,
   so the webServer wiring and the console-problem collector live once here
   rather than being copied twice and drifting. */

import type {
  ConsoleMessage,
  Page,
  PlaywrightTestConfig,
} from "@playwright/test";
import { defineConfig, devices } from "@playwright/test";

export interface WorkerAppConfigOptions {
  /** The port the app's own `e2e:server` script binds `wrangler dev` to. */
  port: number;
}

/**
 * `wrangler dev` (run here via each app's `e2e:server` script) builds the
 * same strict, nonce'd Content-Security-Policy production does - `astro
 * dev` does not (see docs/engineering.md's "Local dev serves 'unsafe-inline'"
 * note), so it is the only local target a CSP regression is observable
 * against. `e2e:server` pins an explicit port rather than reusing `preview`,
 * because both apps' `preview` scripts default to the same wrangler port and
 * Playwright needs to know it up front to poll for readiness.
 *
 * One browser project only: this is a smoke/regression suite (page loads,
 * console is clean, the two documented failure classes above don't recur),
 * not a cross-browser compatibility matrix.
 */
export function createWorkerAppConfig(
  options: WorkerAppConfigOptions,
): PlaywrightTestConfig {
  const baseURL = `http://localhost:${options.port}`;
  return defineConfig({
    testDir: "./e2e",
    testIgnore: "**/*.webview.*",
    fullyParallel: true,
    forbidOnly: Boolean(process.env.CI),
    retries: process.env.CI ? 1 : 0,
    reporter: process.env.CI ? "line" : "list",
    projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
    use: {
      baseURL,
      trace: "on-first-retry",
    },
    webServer: {
      command: "bun run e2e:server",
      url: baseURL,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
  });
}

const CSP_VIOLATION_PATTERN = /content security policy|refused to/i;

export interface ConsoleProblems {
  errors: string[];
  cspViolations: string[];
}

/**
 * Call before the page's first navigation. A CSP violation has no
 * Playwright-level event of its own - it surfaces only as a browser console
 * error - so this is the one place both a JS regression and a CSP
 * regression (e.g. the nonce-mismatch class documented in
 * docs/engineering.md, or a hotlinked asset CSP would reject) are both
 * observable from outside the page.
 */
export function collectConsoleProblems(page: Page): ConsoleProblems {
  const problems: ConsoleProblems = { errors: [], cspViolations: [] };
  page.on("console", (message: ConsoleMessage) => {
    if (message.type() !== "error") return;
    const text = message.text();
    if (CSP_VIOLATION_PATTERN.test(text)) {
      problems.cspViolations.push(text);
    } else {
      problems.errors.push(text);
    }
  });
  page.on("pageerror", (error) => {
    problems.errors.push(error.message);
  });
  return problems;
}
